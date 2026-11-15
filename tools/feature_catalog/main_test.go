package main

import (
	"bytes"
	"os"
	"testing"
)

func TestCatalogDocumentMatchesServerDefinitions(t *testing.T) {
	actual, err := os.ReadFile("../../" + document)
	if err != nil || !bytes.Equal(actual, render()) {
		t.Fatal("feature inventory drift: run go run ./tools/feature_catalog -write", err)
	}
}
