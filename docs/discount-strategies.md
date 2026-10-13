# Discount strategies 與 SQLite 設定

折扣演算法放在 `commerce/domain/discount`；配置數字放在 SQLite `discount_rules`。Application 在商品價格所在的同一筆 Store transaction 讀取 coupon 對應的 Rule，再交給 Engine，依 `strategy` 查 registry 呼叫 Strategy。Domain 沒有 SQL 或 Gin dependency，也沒有寫死 coupon 名稱。

| Seed coupon | strategy | rate_bps | amount_minor | min_subtotal_minor | max_discount_minor |
|---|---|---:|---:|---:|---:|
| WELCOME10 | percentage | 1000 | 0 | 0 | 0 |
| SAVE500 | fixed_amount | 0 | 500 | 5000 | 0 |
| BIGSALE20 | percentage | 2000 | 0 | 10000 | 3000 |

所有 seed 的 currency 是 USD、active 是 1。`rate_bps` 為 basis points，1000 表示折扣 **10%**；money / 門檻 / 上限使用 currency minor units，本例是 USD cents。`max_discount_minor=0` 表示不限上限，僅 percentage strategy 使用可設定的 cap。SAVE500 表示商品小計滿 USD 50 減 USD 5；BIGSALE20 表示滿 USD 100 折 20%，最多減 USD 30。WELCOME10 沒有限制首次購買。

## 計算順序

1. 從 server-side 商品價格算 subtotal。
2. 無 coupon：discount 為 0；有 coupon：讀 SQLite rule，驗證 active / currency / min subtotal。
3. Engine 依 rule.strategy 分派至 PercentageStrategy 或 FixedAmountStrategy。
4. percentage 使用整數 BPS 計算並無條件捨去，先拆整數部分再相乘，避免 int64 overflow；fixed amount 不超過 subtotal。
5. 套用百分比折扣上限，得出 discount_minor。
6. 運費依折扣後小計判斷，再算 total。一次只用一個 coupon，不堆疊折扣。

找不到 coupon、停用、幣別不同或未達門檻，API 回 422 `invalid_request`，不建立訂單、不扣庫存。SQL 故障不會當成「無優惠」繼續結帳。

Quote 讀取目前 DB 設定；checkout 也重新讀取目前設定，Quote 不鎖價。每個新訂單的 `discount` 欄位保存當時完整 Rule snapshot，`discount_minor` 保存實際折扣；重送同 key 返回原訂單，不會因之後改價／調整優惠而重新計價。

## 查詢與改數字

```bash
make seed
sqlite3 -header -column data/ecommerce.db \
  'SELECT code, strategy, rate_bps, amount_minor, min_subtotal_minor, max_discount_minor, currency, active FROM discount_rules;'
```

把 WELCOME10 改為 15%：

```sql
UPDATE discount_rules SET rate_bps = 1500 WHERE code = 'WELCOME10';
```

把 SAVE500 改為滿 USD 60 減 USD 8：

```sql
UPDATE discount_rules
SET amount_minor = 800, min_subtotal_minor = 6000
WHERE code = 'SAVE500';
```

暫停優惠：

```sql
UPDATE discount_rules SET active = 0 WHERE code = 'BIGSALE20';
```

下次 Quote / 新 checkout 立即讀到修改，不需要改 Go code 或重啟。`make seed` / API startup 只插入缺少的 coupon，不會覆蓋已編輯的數字或 active 狀態。SQL 修改範例供本機／管理者使用，尚未提供公開管理 endpoint。

discount_rules 是 STRICT table；CHECK constraints 限制 rate 為 1–10000、amount / threshold / cap 非負，且百分比與固定折扣的欄位不能混用。新增策略需新增 domain Strategy、registry entry、對應 schema migration / constraints 及 tests；數字仍由 DB Rule 提供。不可修改已套用的 002 migration。

`002_discount_strategies.sql` 升級既有 DB，保留所有商品、庫存與訂單，只新增 discount_rules 與 orders.discount_snapshot。舊訂單無法還原當時使用的完整規則，保留原 totals 且不捏造 snapshot。

測試涵蓋兩種策略、最低消費、cap、停用／未知優惠、整數 rounding／overflow、改 DB 立即影響報價、seed 不覆蓋修改、訂單 snapshot / replay 維持原值，以及舊 DB migration。
