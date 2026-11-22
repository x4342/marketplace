"""Exercise server-rendered journeys against a loopback-only onion test network."""

import ipaddress
import os
import re
from urllib.parse import urlsplit

from playwright.sync_api import expect, sync_playwright

from browser_smoke import register_fixture


def main():
    origin = os.environ["MARKETPLACE_TOR_ORIGIN"]
    if not re.fullmatch(r"http://[a-z2-7]{56}\.onion", origin):
        raise SystemExit("Use the HTTP origin of a v3 onion service on the private test network.")
    proxy = os.getenv("MARKETPLACE_TOR_PROXY", "socks5://127.0.0.1:9006")
    address = urlsplit(proxy)
    try:
        valid_proxy = (
            address.scheme == "socks5"
            and ipaddress.ip_address(address.hostname).is_loopback
            and 0 < address.port < 65536
            and address.netloc == proxy.removeprefix("socks5://")
            and address.username is None
            and address.password is None
        )
    except (TypeError, ValueError):
        valid_proxy = False
    if not valid_proxy:
        raise SystemExit("Use the SOCKS port of the local Tor test client.")

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(
            executable_path=os.getenv("MARKETPLACE_CHROMIUM_EXECUTABLE"),
            proxy={"server": proxy},
        )
        context = browser.new_context(
            java_script_enabled=False,
            viewport={"width": 375, "height": 900},
            accept_downloads=True,
        )
        page = context.new_page()
        page.set_default_timeout(15000)
        requests = []
        page.on("request", lambda request: requests.append(request.url))
        page.goto(origin + "/discover?q=keyboard")
        expect(page.get_by_text("Compact mechanical keyboard", exact=True)).to_be_visible()
        username = register_fixture(page, origin, "torbuyer")
        expect(page.get_by_role("heading", name="Save your recovery codes")).to_be_visible()
        page.get_by_role("link", name="Continue to account").click()
        expect(page.get_by_text("Signed in as " + username, exact=True)).to_be_visible()

        listing = origin + "/listings/3cd38877-43d7-403a-94b4-450456bde7ce"
        page.goto(listing)
        page.get_by_role("button", name="Message seller").click()
        page.get_by_label("Message", exact=True).fill("Synthetic private Tor test message")
        page.get_by_role("button", name="Send message").click()
        expect(page.get_by_role("status")).to_contain_text("Message sent")
        expect(page.get_by_text("Synthetic private Tor test message", exact=True)).to_be_visible()
        page.goto(listing)
        page.get_by_role("link", name="Simulated checkout").click()
        page.get_by_role("button", name="Create order and quote").click()
        page.get_by_role("button", name="Simulate and verify payment").click()
        with page.expect_download() as download:
            page.get_by_role("link", name="Download private file").click()
        assert "Workshop inventory fixture" in download.value.path().read_text()
        assert all(urlsplit(url).netloc == urlsplit(origin).netloc for url in requests)
        context.close()
        browser.close()
    print("Private Tor smoke passed with JavaScript disabled.")


if __name__ == "__main__":
    main()
