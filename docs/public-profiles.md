# Public profiles and avatars

A registered account has a public profile at `/users/{username}`. Guests see the username, join date, optional biography, avatar, verified seller rating where applicable, and published listings. Each published listing links to the existing listing-specific contact workflow. Private order activity, purchases, messages, earnings, credentials, and moderation data are outside the public query. An active account restriction hides the profile and substitutes the default avatar. The owner keeps access to private profile settings.

**Account → Edit profile** updates a 500-character biography and optional picture. The avatar editor uses the same crop, orientation and color conversion as M11. New avatars are 256 × 256 SDR JPEGs strictly below 512 KiB. With JavaScript disabled, the server center crops. The limits, formats and color behavior for source uploads follow the [listing media guide](listing-media.md).

Migration 00011 adds the user-owned `user_profiles` table. Only sanitized JPEG bytes are stored. Save and removal atomically replace the single avatar value. Public links keep a stable username URL, and application responses use `no-store`. The existing PostgreSQL volume holds profile media and there are no additional container mounts. Migration rollback refuses to discard populated profile records.

M14 adds indexed public member search and an authenticated direct-message action for eligible peers. Restricted profiles stay absent from search. Members can still find an existing underscore username by its canonical prefix.
