package catalog

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/x4342/marketplace/internal/db"
)

const PageSize = 12

var (
	decimal = regexp.MustCompile(`^[0-9]{1,9}(\.[0-9]{1,2})?$`)
	seller  = regexp.MustCompile(`^[a-z0-9_]{3,30}$`)
)

type Option struct {
	Value string
	Label string
}

var (
	TypeOptions = []Option{{"physical", "Physical goods"}, {"digital", "Digital goods"}}
	AvailabilityOptions = []Option{{"in_stock", "In stock"}, {"orderable", "Available to order"}}
	ConditionOptions = []Option{{"new", "New"}, {"used", "Used"}, {"refurbished", "Refurbished"}}
	RegionOptions = []Option{{"EU", "European Union"}, {"US", "United States"}}
	DeliveryOptions = []Option{{"automatic", "Automatic download"}, {"manual", "Manual delivery"}}
	CurrencyOptions = []Option{{"USD", "USD"}, {"EUR", "EUR"}}
	SortOptions = []Option{{"newest", "Newest"}, {"price_asc", "Price low to high"}, {"price_desc", "Price high to low"}}
)

type Filters struct {
	Values url.Values
	Query db.DiscoverListingsParams
	Page int
	Problems []string
}

func Parse(values url.Values, categories []db.CatalogCategoriesRow) Filters {
	f := Filters{Values: make(url.Values), Page: 1}
	keyword := strings.TrimSpace(values.Get("q"))
	if len(keyword) > 120 {
		f.Problems = append(f.Problems, "Search terms must be 120 bytes or fewer.")
		keyword = ""
	}
	f.set("q", keyword)
	f.Query.Keyword = keyword
	if values.Get("saved") == "1" {
		f.set("saved", "1")
		f.Query.SavedOnly = true
	}
	if values.Get("type") != "all" {
		f.Query.Kind = f.option(values, "type", "listing type", "", TypeOptions)
	}
	f.Query.Availability = f.option(values, "availability", "availability", "", AvailabilityOptions)
	f.Query.DisplayCurrency = f.option(values, "currency", "currency", "USD", CurrencyOptions)

	category := values.Get("category")
	if category != "" {
		if slices.ContainsFunc(categories, func(c db.CatalogCategoriesRow) bool { return c.Slug == category }) {
			f.set("category", category)
			f.Query.Category = category
		} else {
			f.Problems = append(f.Problems, "The category is unavailable.")
		}
	}
	username := values.Get("seller")
	if username != "" {
		if seller.MatchString(username) {
			f.set("seller", username)
			f.Query.Seller = username
		} else {
			f.Problems = append(f.Problems, "Enter a valid seller username.")
		}
	}

	if f.Query.Kind != "digital" {
		f.Query.Condition = f.option(values, "condition", "condition", "", ConditionOptions)
		f.Query.Region = f.option(values, "region", "shipping destination", "", RegionOptions)
	} else if values.Get("condition") != "" || values.Get("region") != "" {
		f.Problems = append(f.Problems, "Physical shipping and condition filters were cleared.")
	}
	if f.Query.Kind != "physical" {
		f.Query.Delivery = f.option(values, "delivery", "delivery method", "", DeliveryOptions)
	} else if values.Get("delivery") != "" {
		f.Problems = append(f.Problems, "Digital delivery filters were cleared.")
	}

	f.Query.Minimum = f.price(values, "min", 0)
	f.Query.Maximum = f.price(values, "max", 1_000_000_000_000)
	if f.Query.Minimum > f.Query.Maximum {
		f.Problems = append(f.Problems, "Minimum price must be no greater than maximum price.")
		f.Query.Minimum = f.Query.Maximum + 1
	}
	defaultSort := "newest"
	options := SortOptions
	if keyword != "" {
		defaultSort = "relevance"
		options = append([]Option{{"relevance", "Relevance"}}, options...)
	}
	f.Query.SortMode = f.option(values, "sort", "sort order", defaultSort, options)
	if raw := values.Get("page"); raw != "" {
		if page, err := strconv.Atoi(raw); err == nil && page >= 1 && page <= 10000 {
			f.Page = page
		} else {
			f.Problems = append(f.Problems, "Page number is outside the supported range.")
		}
	}
	f.Query.SkipCount = int32((f.Page - 1) * PageSize)
	return f
}

func (f *Filters) price(values url.Values, key string, fallback int64) int64 {
	value := strings.TrimSpace(values.Get(key))
	if value == "" {
		return fallback
	}
	if !decimal.MatchString(value) {
		f.Problems = append(f.Problems, "Enter a price with at most two decimal places.")
		return fallback
	}
	major, fraction, _ := strings.Cut(value, ".")
	whole, _ := strconv.ParseInt(major, 10, 64)
	minor, _ := strconv.ParseInt((fraction+"00")[:2], 10, 64)
	f.set(key, value)
	return whole*100 + minor
}

func (f *Filters) option(values url.Values, key, label, defaultValue string, options []Option) string {
	value := values.Get(key)
	if value == "" {
		return defaultValue
	}
	if !slices.ContainsFunc(options, func(option Option) bool { return option.Value == value }) {
		f.Problems = append(f.Problems, "Invalid "+label+" was cleared.")
		return defaultValue
	}
	if value != defaultValue {
		f.set(key, value)
	}
	return value
}

func (f *Filters) set(key, value string) {
	if value != "" {
		f.Values.Set(key, value)
	}
}

func (f Filters) Link(page int, remove ...string) string {
	values := make(url.Values)
	for key, value := range f.Values {
		if !slices.Contains(remove, key) {
			values[key] = value
		}
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	return DiscoverURL(values)
}

func DiscoverURL(values url.Values) string {
	if encoded := values.Encode(); encoded != "" {
		return "/discover?" + encoded
	}
	return "/discover"
}
