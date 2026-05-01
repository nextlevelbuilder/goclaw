package pancake

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderPrivateReplyMessage(t *testing.T) {
	t.Run("empty template falls back to built-in English", func(t *testing.T) {
		got := renderPrivateReplyMessage("", nil)
		if got != defaultPrivateReplyMsg {
			t.Errorf("empty tmpl = %q; want defaultPrivateReplyMsg", got)
		}
		if !strings.Contains(got, "Thanks") {
			t.Errorf("default should mention thanks: %q", got)
		}
	})

	t.Run("single var", func(t *testing.T) {
		got := renderPrivateReplyMessage("Hi {{commenter_name}}", map[string]string{
			"commenter_name": "Tuan",
		})
		if got != "Hi Tuan" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("multiple vars", func(t *testing.T) {
		got := renderPrivateReplyMessage("Hi {{commenter_name}} from {{post_title}}", map[string]string{
			"commenter_name": "Tuan",
			"post_title":     "Xmas sale",
		})
		if got != "Hi Tuan from Xmas sale" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("unknown placeholder left as-is", func(t *testing.T) {
		got := renderPrivateReplyMessage("Hi {{unknown}}", map[string]string{
			"commenter_name": "Tuan",
		})
		if got != "Hi {{unknown}}" {
			t.Errorf("got %q; want placeholder preserved", got)
		}
	})

	t.Run("var value with braces cannot inject new placeholder", func(t *testing.T) {
		got := renderPrivateReplyMessage("Hi {{commenter_name}} from {{post_title}}", map[string]string{
			"commenter_name": "{{post_title}}",
			"post_title":     "Xmas",
		})
		if strings.Contains(got, "{{") || strings.Contains(got, "}}") {
			t.Errorf("render leaked braces: %q", got)
		}
	})

	t.Run("html-like content passes through", func(t *testing.T) {
		got := renderPrivateReplyMessage("Hi {{commenter_name}}", map[string]string{
			"commenter_name": "<script>alert(1)</script>",
		})
		if got != "Hi <script>alert(1)</script>" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("missing vars render placeholder verbatim", func(t *testing.T) {
		got := renderPrivateReplyMessage("Hi {{commenter_name}} from {{post_title}}", map[string]string{
			"commenter_name": "Tuan",
		})
		if got != "Hi Tuan from {{post_title}}" {
			t.Errorf("got %q", got)
		}
	})
}

func TestPancakeConfig_PrivateReplyMessageRoundtrip(t *testing.T) {
	cfg := pancakeInstanceConfig{
		PrivateReplyMessage: "Hi {{commenter_name}}",
	}
	cfg.Features.PrivateReply = true

	buf, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round pancakeInstanceConfig
	if err := json.Unmarshal(buf, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if round.PrivateReplyMessage != "Hi {{commenter_name}}" {
		t.Errorf("message = %q", round.PrivateReplyMessage)
	}
	if !round.Features.PrivateReply {
		t.Errorf("feature flag lost")
	}
}

func TestPancakeConfig_PrivateReplyMessageOmitempty(t *testing.T) {
	cfg := pancakeInstanceConfig{PageID: "p1"}
	buf, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(buf), "private_reply_message") {
		t.Errorf("expected private_reply_message omitted from empty config: %s", buf)
	}
}
