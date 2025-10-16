package main

import (
	"bytes"
	"testing"
)


func TestNewEncryptionKey(t *testing.T) {
	key := newEncryptionKey()
	if len(key) != 32 {
		t.Error("expected 32 bytes")
	}
	
}
func TestCopyEncryptDecrypt(t *testing.T) {


	og_src :="some bytes"
	src := bytes.NewReader([]byte(og_src))
	dst := new(bytes.Buffer)
	key := newEncryptionKey()

	_ , err := copyEncrypt(key, src, dst)
	if err != nil{
		t.Error(err)
	}
	out := new(bytes.Buffer)
	nw, err := copyDecrypt(key, dst, out)
	if err != nil {
		t.Error(err)
	}

	if nw != 16 + len(og_src) {
		t.Error("encryption failed here because of length mismatch")
	}

	if out.String() != og_src {
		t.Error("encryption failed here because of content mismatch")
	}

	


}