package media

import "testing"

func TestIDForURLIsStableAndURLSpecific(t *testing.T) {
	first := IDForURL("https://remote.example/files/photo.png")
	if first != IDForURL("https://remote.example/files/photo.png") {
		t.Fatal("IDForURL is not stable")
	}
	if first == IDForURL("https://remote.example/files/other.png") {
		t.Fatal("different URLs produced the same ID")
	}
	if len(first) != len("media_")+32 {
		t.Fatalf("ID length = %d", len(first))
	}
}
