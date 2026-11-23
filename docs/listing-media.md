# Listing image processing

The seller can select a JPEG, PNG or WebP and choose a square crop. The preview supports drag, touch and keyboard sliders. Confirming the crop requests the exact JPEG from the server and displays it before the draft is saved. Changing position or zoom requires a new confirmation. Cancel clears the selected file. When JavaScript is disabled, saving uses a server-side center crop at the full short dimension.

## Derivative contract

| Property | Behavior |
| --- | --- |
| Source size | Up to 10,000,000 bytes and at least 256 × 256 pixels. Safe decode limits are 36 megapixels and 8192 pixels per side. Landscape, portrait, and square inputs use the same pipeline. |
| Formats | Still JPEG, PNG and WebP. Animated PNG and WebP and unsupported file types are rejected. Export AVIF or HEIC as sRGB JPEG or PNG. |
| Geometry | EXIF orientation is applied before validating crop coordinates. The server accepts three integer values in the oriented source space. The minimum crop is 256 square and is never upscaled. Crops larger than 2560 square are downsampled. |
| Color | Untagged RGB is interpreted as sRGB. RGB matrix/TRC ICC profiles and supported PNG cICP color spaces are converted to sRGB. Full-range RGB PQ and HLG PNG inputs are tone-mapped to SDR. Unhandled profiles, including CMYK and LUT-based ICC, are rejected with export instructions. Transparency uses a neutral matte. |
| Output | A newly encoded, metadata-free 8-bit SDR JPEG. Encoding starts at quality 90 and uses lower quality steps only if needed to stay within the 10 MB cap. |

The reusable `internal/media` package owns byte, dimension, crop and output validation. The HTTP layer checks the signed-in owner and form token before image processing. One image request can decode at a time per process. The preview sends a bounded frame for interactive positioning and the full resulting square on confirmation. It never stores the original. Draft save revalidates the original and crop before storing the derivative. The same pipeline handles both requests.

## Storage and historical compatibility

New photos use the existing `listing_drafts.image_jpeg` and `listing_images.image_jpeg` columns. Migration `00010` widens the size constraints without replacing bytes, IDs, or image references. Historical JPEGs remain byte-for-byte and use square center framing in catalog, listing detail, previews and seller management. A seller can replace one by saving and publishing a new photo. Existing ownership, draft revision, and publication transactions continue to apply. Order snapshots are unaffected.

The existing PostgreSQL volume persists finalized images and draft media across app replacement. The Compose stack requires no new mounts or ports. Use the normal private database backup and recovery procedure.

Profile avatars share this processing pipeline. Migration 00013 leaves media columns and image URLs in place when categories change. The deployment verification compares original and recreated-container responses for both images and avatars. [M13 verification](platform-administration.md)
