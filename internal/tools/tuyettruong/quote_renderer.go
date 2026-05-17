package tuyettruong

import (
	"fmt"
	"strings"
)

// Quote text format (anh-approved). Keep under 1800 chars so a single Zalo OA
// chunk fits comfortably. For very long carts (rare), call splitChunks.

const zaloMaxChars = 1800

type RenderInput struct {
	QuoteCode    string  // short code (used as transferNote, e.g. "Q26051723")
	Draft        *Draft
	ShippingFee  float64 // 0 if unknown / hardcoded
	BankBankName string  // "VCB" or full display name
	BankAccount  string
	BankHolder   string
}

func RenderQuote(in RenderInput) string {
	if in.Draft == nil || len(in.Draft.Items) == 0 {
		return "Báo giá đang trống. Anh/chị muốn thêm sản phẩm gì?"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📋 BÁO GIÁ #Q-%s\n", in.QuoteCode)
	b.WriteString("────────────────────\n")
	subtotal := 0.0
	for i, it := range in.Draft.Items {
		line := it.UnitPriceSnapshot * float64(it.Qty)
		subtotal += line
		attrs := formatAttrs(it.VariantAttributes)
		fmt.Fprintf(&b, "%d. %s%s  ×%d   %s  → %s\n",
			i+1, it.ProductName, attrs, it.Qty,
			formatVnd(it.UnitPriceSnapshot), formatVnd(line),
		)
	}
	b.WriteString("────────────────────\n")
	fmt.Fprintf(&b, "Tạm tính: %s\n", formatVnd(subtotal))
	if in.ShippingFee > 0 {
		fmt.Fprintf(&b, "Phí ship: %s\n", formatVnd(in.ShippingFee))
	}
	total := subtotal + in.ShippingFee
	fmt.Fprintf(&b, "TỔNG:     %s\n\n", formatVnd(total))

	if in.BankBankName != "" && in.BankAccount != "" {
		fmt.Fprintf(&b, "💳 Chuyển khoản:\n  %s • %s • %s\n  Nội dung CK: %s\n\n",
			in.BankBankName, in.BankAccount, in.BankHolder, in.QuoteCode)
	}
	b.WriteString("⏰ Báo giá hiệu lực: 24h\n")
	b.WriteString("Gõ \"đặt\" để xác nhận / \"sửa\" để điều chỉnh.")
	return b.String()
}

// SplitForChannel chunks the rendered text for platforms with strict per-
// message limits (Zalo OA/Personal ≈ 2000). Splits on blank lines first to
// avoid cutting mid-section.
func SplitForChannel(s string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = zaloMaxChars
	}
	if len(s) <= maxChars {
		return []string{s}
	}
	chunks := []string{}
	for len(s) > maxChars {
		// Try to split on the last "\n\n" or "\n" within window.
		cut := strings.LastIndex(s[:maxChars], "\n\n")
		if cut < 0 {
			cut = strings.LastIndex(s[:maxChars], "\n")
		}
		if cut < 0 {
			cut = maxChars
		}
		chunks = append(chunks, strings.TrimSpace(s[:cut]))
		s = strings.TrimSpace(s[cut:])
	}
	if s != "" {
		chunks = append(chunks, s)
	}
	return chunks
}

func formatAttrs(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	parts := []string{}
	for _, k := range []string{"COLOR", "SIZE", "DUNG_TICH"} {
		if v, ok := m[k]; ok && v != "" {
			parts = append(parts, v)
		}
	}
	// any other attrs
	for k, v := range m {
		if k == "COLOR" || k == "SIZE" || k == "DUNG_TICH" {
			continue
		}
		if v != "" {
			parts = append(parts, v)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, "/")
}

// formatVnd formats with thousand separators using dots (VN convention) and
// trailing đ. e.g. 250000 → "250.000đ".
func formatVnd(v float64) string {
	n := int64(v)
	if n < 0 {
		return "-" + formatVnd(-v)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s + "đ"
	}
	// Insert dots from the right every 3 digits.
	var b strings.Builder
	mod := len(s) % 3
	if mod > 0 {
		b.WriteString(s[:mod])
		if len(s) > mod {
			b.WriteByte('.')
		}
	}
	for i := mod; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte('.')
		}
	}
	b.WriteString("đ")
	return b.String()
}
