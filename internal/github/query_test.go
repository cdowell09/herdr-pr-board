package github

import (
	"reflect"
	"testing"
)

func TestSplitQueryPreservesQuotedTerms(t *testing.T) {
	terms, err := splitQuery(`is:open label:"needs review" -is:draft`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"is:open", `label:"needs review"`, "-is:draft"}
	if !reflect.DeepEqual(terms, want) {
		t.Fatalf("terms = %#v, want %#v", terms, want)
	}
}

func TestSplitQueryRejectsUnclosedQuote(t *testing.T) {
	if _, err := splitQuery(`is:open label:"needs review`); err == nil {
		t.Fatal("expected an unclosed-quote error")
	}
}
