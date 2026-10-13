# SQLite 商品 seed 與持久化

`make seed` 建立 `data/ecommerce.db`，執行 schema migration，再插入兩個商品與初始庫存。這個命令不需要 JWT_SECRET，也不啟動 HTTP。API 啟動也會自動完成相同初始化。

| 商品 ID | 名稱 | 金額（USD cents） | 初始庫存 |
|---|---|---:|---:|
| keyboard | Mechanical keyboard | 7500 | 10 |
| mouse | Wireless mouse | 2500 | 20 |

```bash
make seed
sqlite3 data/ecommerce.db \
  'SELECT p.id, p.name, p.price_minor, p.currency, i.available FROM products p JOIN inventory i ON i.product_id = p.id;'
```

`DB_PATH` 可透過 `.env` 或 process environment 指定。若存在 `.env`，Makefile 會讀取它，因此 CLI 可以另外使用明確 flag：

```bash
go run ./apps/api/cmd/db -db ./data/custom.db seed
```

seed SQL 位於 `modules/commerce/adapters/sqlite/seed.sql`，以 `ON CONFLICT DO NOTHING` 只補缺少 rows。重跑 migration / seed 不會重新補滿庫存，不會更新商品名稱、價格或 active 狀態，也不會刪除訂單。要補庫存或改價應另外加入明確的管理 use case；不要把 seed 當成補貨機制。

identity 在同一個 DB file 使用 auth_users / auth_sessions / auth_refresh_tokens 與獨立 migration 表，詳見 [登入設計](authentication.md)。

初始 schema 位於 `migrations/001_initial.sql`，含 products、inventory、orders、order_lines、checkout_receipts、outbox；`002_discount_strategies.sql` 新增 discount_rules 與訂單優惠 snapshot（詳見 [Discount strategies](discount-strategies.md)）；`schema_migrations` 追蹤已套用版本。Migration 與 seed 各自使用 atomic SQL transaction。已套用的 migration 不要修改，新增下一個 `NNN_description.sql`。目前是 demo 自動 migration，正式部署需要獨立 migration policy 與備份策略。

預設 adapter 使用純 Go [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite@v1.60.1)，維持 `CGO_ENABLED=0`；SQL driver 僅由 commerce adapter 引用。每條 connection 都設定 foreign keys、WAL、busy timeout 5000ms、synchronous FULL。`_txlock=immediate` 讓交易在讀取 receipt 或扣庫存前取得 SQLite writer lock，配合條件式 UPDATE 及 receipt UNIQUE key 防止兩個 Store 的併發超賣／重複建單。[SQLite transaction 文件](https://www.sqlite.org/lang_transaction.html)

這個簡單實作的 read transaction 也使用 immediate lock，且每個 Store 的 pool 只有一條 connection，因此讀寫會序列化；不是大量併發流量的配置。跨 process 的 writer 也會排隊，超過 busy timeout 仍會回傳 error。

商品、價格快照、庫存扣減、order、receipt、outbox 同一 SQL transaction；任何 SQL failure、context cancellation 或 callback panic 都會 rollback。ports 的每個 SQL-capable 方法都回傳 error，避免 DB 故障被解讀成空商品清單或不存在的 receipt。

`go test -race` 覆蓋：seed 可重跑、修改過的商品與庫存不重設、重啟後訂單／receipt 仍存在、價格快照不受改價影響、foreign keys、outbox 寫入失敗時完整回滾、取消／panic 回滾、兩個 Store 同時結帳的庫存及冪等正確性。

`make build && make smoke-persistence` 使用 Python 3 標準函式庫啟動 API、建立訂單、正常關閉並重啟 API，再確認同 key 回傳原訂單且庫存只扣一次。測試使用臨時 SQLite file 與 localhost 隨機 port，不修改你的 `data/ecommerce.db`；CI 也會執行。

`/readyz` 會 ping SQLite connection，失敗回 503。資料檔由 CLI / API 建立，忽略於 Git 與 Docker build context；Docker demo 用 `ecommerce-api-data` volume 保留資料。不要在 SQLite 開啟時單獨複製 `.db` 忽略 WAL；請使用 SQLite backup API / CLI `.backup`，或停止所有 writers 後完整備份。
