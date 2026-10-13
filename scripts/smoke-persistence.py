#!/usr/bin/env python3
"""Verify the actual API preserves checkout state across process restart."""
import json
import os
from pathlib import Path
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
TOKEN = "persistence-smoke-token-123456"


def main():
    with tempfile.TemporaryDirectory(prefix="ecommerce-persistence-") as directory:
        database = Path(directory) / "shop.db"
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        base = f"http://127.0.0.1:{port}"
        env = dict(os.environ, APP_ENV="development", API_TOKEN=TOKEN,
                   API_SUBJECT="smoke-customer", HTTP_ADDR=f"127.0.0.1:{port}",
                   DB_PATH=str(database))
        order_id = None
        for attempt in range(2):
            with (Path(directory) / f"api-{attempt}.log").open("w") as log:
                process = subprocess.Popen([str(ROOT / "bin/api")], env=env,
                                           stdout=log, stderr=subprocess.STDOUT)
                try:
                    for _ in range(100):
                        if process.poll() is not None:
                            raise RuntimeError("API exited before readiness")
                        try:
                            with urllib.request.urlopen(base + "/readyz", timeout=1) as response:
                                assert json.load(response)["storage"] == "sqlite"
                            break
                        except (urllib.error.URLError, TimeoutError):
                            time.sleep(0.1)
                    else:
                        raise RuntimeError("API readiness timed out")
                    for coupon, quantity, expected in (("WELCOME10", 1, 750), ("SAVE500", 1, 500), ("BIGSALE20", 3, 3000)):
                        quote_request = urllib.request.Request(base + "/v1/quotes",
                            data=json.dumps({"items": [{"product_id": "keyboard", "quantity": quantity}], "coupon": coupon}).encode(),
                            headers={"Authorization": "Bearer " + TOKEN, "Content-Type": "application/json"})
                        with urllib.request.urlopen(quote_request, timeout=5) as response:
                            assert json.load(response)["discount_minor"] == expected
                    body = json.dumps({"items": [{"product_id": "keyboard", "quantity": 1}], "coupon": "WELCOME10"}).encode()
                    request = urllib.request.Request(base + "/v1/checkouts", data=body,
                        headers={"Authorization": "Bearer " + TOKEN,
                                 "Content-Type": "application/json",
                                 "Idempotency-Key": "persistence-request-001"})
                    with urllib.request.urlopen(request, timeout=5) as response:
                        assert response.status == (201 if attempt == 0 else 200)
                        result = json.load(response)
                    assert result["replayed"] == (attempt == 1)
                    assert result["order"]["discount_minor"] == 750
                    assert result["order"]["discount"]["rate_bps"] == 1000
                    if attempt == 0:
                        order_id = result["order"]["id"]
                    else:
                        assert result["order"]["id"] == order_id
                finally:
                    if process.poll() is None:
                        process.terminate()
                    try:
                        process.wait(timeout=15)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait()
            assert process.returncode == 0, f"API stopped with {process.returncode}"
        with sqlite3.connect(database) as db:
            assert db.execute("SELECT available FROM inventory WHERE product_id = 'keyboard'").fetchone()[0] == 9
            for table in ("orders", "checkout_receipts", "outbox"):
                assert db.execute(f"SELECT count(*) FROM {table}").fetchone()[0] == 1
        print("SQLite persistence smoke passed: discount strategies, order snapshots, and idempotency survive restart")


if __name__ == "__main__":
    main()
