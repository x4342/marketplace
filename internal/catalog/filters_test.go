package catalog

import (
	"net/url"
	"strings"
	"testing"

	"github.com/x4342/marketplace/internal/db"
)

func TestFiltersAndCanonicalLinks(t *testing.T) {
	categories := []db.CatalogCategoriesRow{{Slug: "equipment", Name: "Equipment"}}
	values := url.Values{
		"q": {"keyboard"}, "type": {"digital"}, "condition": {"used"},
		"category": {"equipment"}, "page": {"2"}, "min": {"12.1"}, "currency": {"EUR"},
	}
	f := Parse(values, categories)
	if f.Query.Condition != "" || f.Query.SortMode != "relevance" || f.Query.DisplayCurrency != "EUR" ||
		f.Query.Minimum != 1210 || f.Query.SkipCount != 12 || len(f.Problems) != 1 {
		t.Fatalf("unexpected validated filters: %+v", f)
	}
	link := f.Link(3, "category")
	for _, wanted := range []string{"q=keyboard", "type=digital", "currency=EUR", "page=3", "min=12.1"} {
		if !strings.Contains(link, wanted) {
			t.Errorf("%s missing from %s", wanted, link)
		}
	}
	if strings.Contains(link, "category") || strings.Contains(link, "condition") {
		t.Errorf("removed filters remained in %s", link)
	}
}

func TestFiltersRejectInvalidValues(t *testing.T) {
	input := url.Values{
		"min": {"22"}, "max": {"5"}, "currency": {"XYZ"},
		"sort": {"relevance"}, "page": {"0"}, "category": {"missing"},
	}
	f := Parse(input, nil)
	if f.Query.Minimum <= f.Query.Maximum || f.Query.SortMode != "newest" ||
		f.Query.DisplayCurrency != "USD" || f.Page != 1 || len(f.Problems) != 5 {
		t.Fatalf("unexpected invalid filter handling: %+v", f)
	}
}
