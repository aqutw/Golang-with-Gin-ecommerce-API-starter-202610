#!/usr/bin/env python3
"""Create the local JWT signing secret without printing or committing it."""
import base64
import os
from pathlib import Path

root = Path(__file__).resolve().parents[1]
path = root / ".env"
source = path.read_text() if path.exists() else (root / ".env.example").read_text()
lines = source.splitlines()
found = False
for index, line in enumerate(lines):
    if line.startswith("JWT_SECRET="):
        found = True
        if not line.partition("=")[2].strip():
            lines[index] = "JWT_SECRET=" + base64.b64encode(os.urandom(32)).decode()
        break
if not found:
    lines.append("JWT_SECRET=" + base64.b64encode(os.urandom(32)).decode())
path.write_text("\n".join(lines) + "\n")
os.chmod(path, 0o600)
print("Local .env ready; existing settings preserved, JWT secret not displayed")
