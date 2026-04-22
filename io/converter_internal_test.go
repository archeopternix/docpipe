package io

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOfficeConvertersSerialize(t *testing.T) {
	path := filepath.Join("..", "TestData", "Strategy.docx")

	officeMu.Lock()

	started := make(chan struct{})
	done := make(chan struct{})

	go func() {
		close(started)
		var meta MetaData
		_, _ = WordFileConverter(path, &meta)
		close(done)
	}()

	<-started
	select {
	case <-done:
		officeMu.Unlock()
		t.Fatal("WordFileConverter completed while office mutex was held")
	case <-time.After(150 * time.Millisecond):
	}

	officeMu.Unlock()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WordFileConverter did not resume after office mutex release")
	}
}
