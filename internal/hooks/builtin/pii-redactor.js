// Builtin: PII redactor (email + phone)
// Version: 1 — matches builtins.yaml
// Runs on: user_prompt_submit, pre_tool_use
// Mutates: rawInput, toolInput.command, toolInput.query, toolInput.content
//
// ES5.1 only — goja runtime. No let/const, no arrow functions, no template literals.

var EMAIL_RE = /[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}/g;
// E.164-ish: optional +, leading 1-9, 7-14 more digits; guarded so embedded
// digits inside long tokens (ids, paths, filenames) are left alone.
var PHONE_RE = /(^|[^0-9+])(\+?[1-9][0-9]{7,14})(?=[^0-9]|$)/g;

function redact(s) {
    if (typeof s !== "string" || s.length === 0) return s;
    var out = s.replace(EMAIL_RE, "[REDACTED_EMAIL]");
    out = out.replace(PHONE_RE, function (_m, lead) {
        return lead + "[REDACTED_PHONE]";
    });
    return out;
}

function handle(event) {
    var changed = false;
    var updated = { toolInput: {} };

    if (typeof event.rawInput === "string") {
        var r = redact(event.rawInput);
        if (r !== event.rawInput) {
            updated.rawInput = r;
            changed = true;
        }
    }

    var fields = ["command", "query", "content"];
    var ti = event.toolInput || {};
    for (var i = 0; i < fields.length; i++) {
        var k = fields[i];
        var v = ti[k];
        if (typeof v === "string") {
            var rv = redact(v);
            if (rv !== v) {
                updated.toolInput[k] = rv;
                changed = true;
            }
        }
    }

    if (!changed) {
        return { decision: "allow" };
    }
    var tiKeys = Object.keys(updated.toolInput);
    if (tiKeys.length === 0) {
        delete updated.toolInput;
    }
    return { decision: "allow", updatedInput: updated };
}
