---
name: tuyet-truong-nhap-hang
description: "Quy trình nhập hàng (import goods) cho shop: tiếp viên gửi bill/hoá đơn (vd Chemist Warehouse Úc), agent đọc line items + giá ĐÃ TRẢ, chốt tỉ giá trong ngày, tính giá vốn về VND + đề xuất giá bán, rồi lên hàng (tạo draft INACTIVE cho hàng mới / cộng kho cho hàng cũ). Triggers: 'nhập hàng', 'bill', 'hoá đơn', 'lên hàng', 'tiếp viên gửi bill', 'có hàng về', 'tính giá vốn', 'chốt tỉ giá'."
license: Proprietary
---

# tuyet-truong-nhap-hang — Quy trình nhập hàng

Tiếp viên (người xách tay / forwarder) gửi bill mua hàng ở nước ngoài. Agent đọc bill, tính giá vốn về VND, đề xuất giá bán, rồi lên hàng. **Giá bán và việc lên hàng PHẢI được admin/tiếp viên xác nhận — không bao giờ tự đăng bán.** Khách không bao giờ thấy giá vốn / tỉ giá.

Tools dùng: `shop_fx_get`, `shop_fx_set_snapshot`, `shop_suppliers_list`, `shop_nhaphang_price`, `shop_nhaphang_commit`.

## Tiền tệ theo xuất xứ

| Currency | Xuất xứ |
|---|---|
| AUD | Úc |
| KRW | Hàn |
| JPY | Nhật |
| EUR | Đức |
| USD | Mỹ |

## Quy trình

### 1) Đọc bill (ảnh)
- Nhận ẢNH bill (vd Chemist Warehouse Úc). Đọc từng dòng: **tên sản phẩm + giá ĐÃ TRẢ** (cột paid). **Bỏ qua "Why Pay" / RRP** — đó chỉ là giá niêm yết tham khảo.
- **Gộp dòng trùng** (cùng sản phẩm → cộng dồn qty).
- Đọc size/khối lượng để ước lượng cân nặng (kg) cho phí ship.

### 2) Xác định tiền tệ
- Suy ra từ xuất xứ bill: store Úc → AUD, store Hàn → KRW, v.v. (bảng trên).

### 3) Hỏi tiếp viên — GỘP TRONG 1 TIN NHẮN
Hỏi một lần cho gọn:
1. **Tỉ giá hôm nay**: gọi `shop_fx_get` lấy `suggested` (= VCB Sell + buffer) rồi đưa con số đó cho tiếp viên xác nhận.
2. **Phí ship/kg** nếu chưa biết (hoặc dùng `carryFeePerKg` mặc định từ `shop_suppliers_list`).
3. **Những món nào là của shop** (lọc món cá nhân ra khỏi bill).

### 4) Chốt tỉ giá rồi tính giá
- Sau khi tiếp viên xác nhận tỉ giá → gọi `shop_fx_set_snapshot` (`source: "supplier"` — supplier-confirmed là authoritative).
- Gọi `shop_nhaphang_price` với `lines` + `currency` (+ `supplierId` nếu có) để tính:
  - **giá vốn** = `unitPrice × fxRate + weightKg × carryFeePerKg`
  - **giá bán đề xuất** = giá vốn × (1 + markup), làm tròn.
- Đọc `existingMatch` của mỗi item: khác null = đã có trong catalog; null = hàng mới.

### 5) Phân loại item
- **Hàng đã có** (`existingMatch != null`) → **cộng kho, KHÔNG đổi giá**. Báo `costDelta` (chênh lệch giá vốn so với lần trước) cho admin.
- **Hàng mới** (`existingMatch == null`) → tạo **draft INACTIVE** để admin duyệt.

### 6) Xác nhận giá rồi lên hàng
- Giá bán **PHẢI được admin/tiếp viên xác nhận** trước khi gọi `shop_nhaphang_commit`. Không bao giờ tự đăng bán.
- Gọi `shop_nhaphang_commit` với `items` (đặt `existingSlug` cho hàng cũ để cộng kho; bỏ trống cho hàng mới để tạo draft).
- **Khách không bao giờ thấy giá vốn / tỉ giá.**

## Ví dụ (bill Chemist Warehouse, Úc → AUD)

Tiếp viên gửi bill, có dòng: **Chia Seed — paid 6.99 AUD** (Why Pay 9.99 — bỏ qua).

1. `shop_fx_get(currencies=["AUD"])` → `suggested ≈ 19700`. Đưa cho tiếp viên: "Tỉ giá AUD hôm nay 19.700đ ạ, chị confirm giúp em nha."
2. Tiếp viên OK → `shop_fx_set_snapshot(currency="AUD", rate=19700, source="supplier")`.
3. `shop_nhaphang_price(lines=[{name:"Chia Seed", unitPrice:6.99, whyPay:9.99, qty:1}], currency="AUD", markupPctOverride:30, roundTo:1000)`
   - giá vốn ≈ 6.99 × 19700 + phí kg ≈ ~138.000đ + ship
   - giá bán đề xuất +30% → **~199.000đ** (làm tròn).
4. Admin/tiếp viên xác nhận 199.000đ.
5. Hàng mới → `shop_nhaphang_commit(items=[{name:"Chia Seed", costAmount:6.99, costCurrency:"AUD", fxRate:19700, costVnd:138000, retailVnd:199000, qty:1}])` → tạo draft INACTIVE.

## Lưu ý

- Luôn dùng **giá đã trả**, bỏ "Why Pay".
- Tỉ giá tiếp viên xác nhận là authoritative — ưu tiên hơn VCB.
- Hàng đã có: chỉ cộng kho, giữ nguyên giá; báo cost delta để admin biết giá vốn đã đổi.
- Không tự publish; không lộ giá vốn/tỉ giá cho khách.
