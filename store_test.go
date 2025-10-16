package main

import (
	"bytes"
	"io/ioutil"	
	"fmt"
	"testing"
	
)

func TestPathTransformFuncs(t *testing.T) {
	key := "momsbestpicture"

	pathKey := CASPathTransformFunc(key)
	expectedOriginalKey := "6804429f74181a63c50c3d81d733a12f14a353ff"
	expectedPath := "68044/29f74/181a6/3c50c/3d81d/733a1/2f14a/353ff"
	if pathKey.FileName != expectedOriginalKey || pathKey.PathName != expectedPath {
		t.Errorf("expected %s, got %s", expectedPath, pathKey.PathName)
	}

}






func TestDelete(t *testing.T) {
	opts := StoreOpts{
		PathTransformFunc: CASPathTransformFunc,
	}
	s := NewStore(opts)
	key := "myspecialpictures"
	data := []byte("some jpeg bytes")
	_, err := s.writeStream(key, bytes.NewReader(data), 0)
	if err != nil {
		t.Error(err)
	}
	err = s.Delete(key)
	if err != nil {
		t.Error(err)
	}
}




func newStore() *Store {
	opts := StoreOpts{
		PathTransformFunc: CASPathTransformFunc,
	}
	s := NewStore(opts)
	return s
	
}


func tearDown(t *testing.T, s *Store) {
	if err := s.Clear(); err != nil {
		t.Error(err)
	}
}
func TestStore(t *testing.T) {
	s := newStore()
	defer tearDown(t, s)
	count := 50
	for i := 0; i < count; i++ {
	key := fmt.Sprintf("mykey%d", i)
	data := []byte("some jpeg bytes")
	_, err := s.writeStream(key, bytes.NewReader(data), 0)
	if err != nil {
		t.Error(err)
	}



	if ok := s.HasKey(key); !ok {
		t.Errorf("key does not exist")
	}
	n, r, err := s.Read(key)
	if err != nil {
		t.Error(err)
	}

	b, _ := ioutil.ReadAll(r)
	if string(b) != string(data) {
		t.Errorf("expected data : %s, got data from reading: %s of size : %d" , data, b, n)
	}
	fmt.Println("this is the data:", string(b))

	if err := s.Delete(key); err != nil { t.Error(err) }
	if ok := s.HasKey(key); ok {
		t.Errorf("key should have been deleted")
	}
}


	
}