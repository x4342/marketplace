package seller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x4342/marketplace/internal/db"
)

const MaxUpload = 3 << 20

var ErrConflict = errors.New("listing draft changed")
var decimalPrice = regexp.MustCompile(`^[0-9]{1,9}(\.[0-9]{1,2})?$`)

type Fields struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Kind        string `json:"kind"`
	Price       string `json:"price"`
	Currency    string `json:"currency"`
	Stock       string `json:"stock"`
	Orderable   bool   `json:"orderable"`
	Condition   string `json:"condition"`
	RegionEU    bool   `json:"region_eu"`
	RegionUS    bool   `json:"region_us"`
	Delivery    string `json:"delivery"`
	Terms       string `json:"terms"`
}

func FromForm(form url.Values) Fields {
	value := func(key string) string { return strings.TrimSpace(form.Get(key)) }
	return Fields{
		Title: value("title"), Description: value("description"), Category: value("category"),
		Kind: value("kind"), Price: value("price"), Currency: value("currency"),
		Stock: value("stock"), Orderable: value("orderable") == "1", Condition: value("condition"),
		RegionEU: value("region_eu") == "1", RegionUS: value("region_us") == "1",
		Delivery: value("delivery"), Terms: value("terms"),
	}
}

func (f Fields) SizeErrors() []string {
	var problems []string
	if utf8.RuneCountInString(f.Title) > 140 {
		problems = append(problems, "Use at most 140 characters for the title.")
	}
	if utf8.RuneCountInString(f.Description) > 3000 || utf8.RuneCountInString(f.Terms) > 3000 {
		problems = append(problems, "Use at most 3,000 characters each for the description and terms.")
	}
	for _, v := range []string{f.Category, f.Kind, f.Price, f.Currency, f.Stock, f.Condition, f.Delivery} {
		if len(v) > 120 {
			problems = append(problems, "Use the provided options and short numeric values.")
			break
		}
	}
	return problems
}

func (f Fields) Publication(id, owner pgtype.UUID, categories []db.CatalogCategoriesRow) (db.PublishListingParams, []string) {
	p := db.PublishListingParams{
		ID: id, SellerID: owner, Title: f.Title, Description: f.Description, CategorySlug: f.Category,
		Kind: f.Kind, Currency: f.Currency, CanBackorder: f.Orderable, Terms: f.Terms, ShipRegions: []string{},
	}
	problems := f.SizeErrors()
	if f.Title == "" || f.Description == "" || f.Terms == "" {
		problems = append(problems, "Enter a title, description, and fulfillment terms.")
	}
	if !slices.ContainsFunc(categories, func(c db.CatalogCategoriesRow) bool { return c.Slug == f.Category }) {
		problems = append(problems, "Choose a current category.")
	}
	if f.Currency != "USD" && f.Currency != "EUR" {
		problems = append(problems, "Choose USD or EUR.")
	}
	if decimalPrice.MatchString(f.Price) {
		whole, decimals, _ := strings.Cut(f.Price, ".")
		major, _ := strconv.ParseInt(whole, 10, 64)
		minor, _ := strconv.ParseInt((decimals + "00")[:2], 10, 64)
		p.PriceMinor = major*100 + minor
	}
	if p.PriceMinor == 0 {
		problems = append(problems, "Enter a price greater than zero with up to two decimal places.")
	}
	stock, err := strconv.ParseInt(f.Stock, 10, 32)
	if err != nil || stock < 0 {
		problems = append(problems, "Enter stock as a nonnegative whole number.")
	}
	p.Stock = int32(stock)
	switch f.Kind {
	case "physical":
		p.Condition = f.Condition
		if f.RegionEU {
			p.ShipRegions = append(p.ShipRegions, "EU")
		}
		if f.RegionUS {
			p.ShipRegions = append(p.ShipRegions, "US")
		}
		if !slices.Contains([]string{"new", "used", "refurbished"}, f.Condition) || len(p.ShipRegions) == 0 {
			problems = append(problems, "Choose a physical condition and at least one shipping region.")
		}
	case "digital":
		p.DeliveryMode = f.Delivery
		if f.Delivery != "automatic" && f.Delivery != "manual" {
			problems = append(problems, "Choose a digital delivery method.")
		}
	default:
		problems = append(problems, "Choose physical or digital.")
	}
	return p, problems
}

func ReadImage(source io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(source, MaxUpload+1))
	if err != nil || len(raw) > MaxUpload {
		return nil, errors.New("Upload a JPEG or PNG of 3 MB or less.")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "jpeg" && format != "png") || config.Width < 1 || config.Height < 1 ||
		config.Width > 2048 || config.Height > 2048 {
		return nil, errors.New("Upload a JPEG or PNG no larger than 2048 pixels on either side.")
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("This image could not be decoded.")
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, decoded, &jpeg.Options{Quality: 82}); err != nil || output.Len() > MaxUpload {
		return nil, errors.New("Use a smaller or simpler image.")
	}
	return output.Bytes(), nil
}

type Service struct {
	Pool *pgxpool.Pool
}

func (s Service) Publish(ctx context.Context, id, owner pgtype.UUID, revision int64) ([]string, error) {
	var problems []string
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		restricted, err := q.UserRestricted(ctx, owner)
		if err != nil {
			return err
		}
		if restricted {
			return ErrConflict
		}
		draft, err := q.LockListingDraft(ctx, db.LockListingDraftParams{ID: id, SellerID: owner})
		if err != nil {
			return err
		}
		if draft.Revision != revision {
			return ErrConflict
		}
		var fields Fields
		if err := json.Unmarshal(draft.Fields, &fields); err != nil {
			return err
		}
		categories, err := q.CatalogCategories(ctx)
		if err != nil {
			return err
		}
		params, invalid := fields.Publication(id, owner, categories)
		if len(invalid) > 0 {
			problems = invalid
			return nil
		}
		changed, err := q.PublishListing(ctx, params)
		if err != nil {
			return err
		}
		if changed != 1 {
			return ErrConflict
		}
		if len(draft.ImageJpeg) > 0 {
			err = q.PutListingImage(ctx, db.PutListingImageParams{ListingID: id, ImageJpeg: draft.ImageJpeg})
		} else {
			err = q.DeleteListingImage(ctx, id)
		}
		if err != nil {
			return err
		}
		return q.ConsumeListingDraft(ctx, db.ConsumeListingDraftParams{ID: id, SellerID: owner})
	})
	return problems, err
}
