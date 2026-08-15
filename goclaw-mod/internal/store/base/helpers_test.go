package base

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNilStr_Empty(t *testing.T) {
	if got := NilStr(""); got != nil {
		t.Errorf("NilStr(\"\") = %v, want nil", got)
	}
}

func TestNilStr_Value(t *testing.T) {
	got := NilStr("x")
	if got == nil || *got != "x" {
		t.Errorf("NilStr(\"x\") = %v, want &\"x\"", got)
	}
}

func TestNilInt_Zero(t *testing.T) {
	if got := NilInt(0); got != nil {
		t.Errorf("NilInt(0) = %v, want nil", got)
	}
}

func TestNilInt_Value(t *testing.T) {
	got := NilInt(5)
	if got == nil || *got != 5 {
		t.Errorf("NilInt(5) = %v, want &5", got)
	}
}

func TestNilUUID_Nil(t *testing.T) {
	nilUUID := uuid.Nil
	if got := NilUUID(&nilUUID); got != nil {
		t.Errorf("NilUUID(&uuid.Nil) = %v, want nil", got)
	}
}

func TestNilUUID_NilPointer(t *testing.T) {
	if got := NilUUID(nil); got != nil {
		t.Errorf("NilUUID(nil) = %v, want nil", got)
	}
}

func TestNilUUID_Value(t *testing.T) {
	valid := uuid.New()
	got := NilUUID(&valid)
	if got == nil || *got != valid {
		t.Errorf("NilUUID(&valid) = %v, want &%s", got, valid)
	}
}

func TestNilTime_Nil(t *testing.T) {
	if got := NilTime(nil); got != nil {
		t.Errorf("NilTime(nil) = %v, want nil", got)
	}
}

func TestNilTime_Zero(t *testing.T) {
	zero := time.Time{}
	if got := NilTime(&zero); got != nil {
		t.Errorf("NilTime(&zero) = %v, want nil", got)
	}
}

func TestNilTime_Value(t *testing.T) {
	now := time.Now()
	got := NilTime(&now)
	if got == nil || !got.Equal(now) {
		t.Errorf("NilTime(&now) = %v, want &%s", got, now)
	}
}

func TestDerefStr_Nil(t *testing.T) {
	if got := DerefStr(nil); got != "" {
		t.Errorf("DerefStr(nil) = %q, want \"\"", got)
	}
}

func TestDerefStr_Value(t *testing.T) {
	s := "x"
	if got := DerefStr(&s); got != "x" {
		t.Errorf("DerefStr(&\"x\") = %q, want \"x\"", got)
	}
}

func TestDerefInt_Nil(t *testing.T) {
	if got := DerefInt(nil); got != 0 {
		t.Errorf("DerefInt(nil) = %d, want 0", got)
	}
}

func TestDerefInt_Value(t *testing.T) {
	v := 42
	if got := DerefInt(&v); got != 42 {
		t.Errorf("DerefInt(&42) = %d, want 42", got)
	}
}

func TestDerefUUID_Nil(t *testing.T) {
	if got := DerefUUID(nil); got != uuid.Nil {
		t.Errorf("DerefUUID(nil) = %s, want uuid.Nil", got)
	}
}

func TestDerefBytes_Nil(t *testing.T) {
	if got := DerefBytes(nil); got != nil {
		t.Errorf("DerefBytes(nil) = %v, want nil", got)
	}
}

func TestDerefBytes_Value(t *testing.T) {
	data := []byte("hello")
	got := DerefBytes(&data)
	if string(got) != "hello" {
		t.Errorf("DerefBytes = %q, want \"hello\"", got)
	}
}

func TestJsonOrEmpty_Nil(t *testing.T) {
	got := JsonOrEmpty(nil)
	if string(got) != "{}" {
		t.Errorf("JsonOrEmpty(nil) = %q, want \"{}\"", got)
	}
}

func TestJsonOrEmpty_Value(t *testing.T) {
	data := []byte(`{"a":1}`)
	got := JsonOrEmpty(data)
	if string(got) != `{"a":1}` {
		t.Errorf("JsonOrEmpty(data) = %q, want %q", got, data)
	}
}

func TestJsonOrEmptyArray_Nil(t *testing.T) {
	got := JsonOrEmptyArray(nil)
	if string(got) != "[]" {
		t.Errorf("JsonOrEmptyArray(nil) = %q, want \"[]\"", got)
	}
}

func TestJsonOrNull_Nil(t *testing.T) {
	got := JsonOrNull(nil)
	if got != nil {
		t.Errorf("JsonOrNull(nil) = %v, want nil", got)
	}
}

func TestJsonOrNull_Value(t *testing.T) {
	data := json.RawMessage(`{"b":2}`)
	got := JsonOrNull(data)
	b, ok := got.([]byte)
	if !ok {
		t.Fatalf("JsonOrNull(data) type = %T, want []byte", got)
	}
	if string(b) != `{"b":2}` {
		t.Errorf("JsonOrNull(data) = %q, want %q", b, data)
	}
}
