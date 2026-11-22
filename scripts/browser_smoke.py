"""Check the built app against the running local PostgreSQL fixture."""

import os
import secrets
from urllib.parse import urlsplit

from playwright.sync_api import expect, sync_playwright


def verify_layout(browser, base_url, width):
    context = browser.new_context(
        viewport={"width": width, "height": 900}, java_script_enabled=False
    )
    page = context.new_page()
    requests = []
    page.on("request", lambda request: requests.append(request.url))

    page.goto(base_url)
    heading = page.get_by_role("heading", name="Overview")
    expect(heading).to_be_visible()
    page.keyboard.press("Tab")
    expect(page.get_by_role("link", name="Skip to content")).to_be_focused()
    page.keyboard.press("Enter")
    expect(page.get_by_role("main")).to_be_focused()

    for theme in ("Dark", "Light"):
        page.get_by_role("button", name=theme, exact=True).click()
        page.reload()
        expect(page.locator("html")).to_have_attribute("data-theme", theme.lower())
        expect(page.get_by_role("button", name=theme, exact=True)).to_have_attribute(
            "aria-pressed", "true"
        )
        viewport = page.locator("html").bounding_box()
        status = page.get_by_role("region", name="System status").bounding_box()
        assert viewport and status
        assert viewport["width"] == width
        assert status["x"] >= 0 and status["x"] + status["width"] <= width
        page.screenshot(path=f"bin/smoke/{width}-{theme.lower()}.png")

    with page.expect_navigation():
        page.get_by_role("button", name="Refresh status").click()
    expect(page.get_by_text("Active", exact=True)).to_be_visible()
    assert all(urlsplit(url).netloc == urlsplit(base_url).netloc for url in requests)
    context.close()


def verify_htmx(browser, base_url):
    page = browser.new_page()
    page.goto(base_url)
    with page.expect_response(lambda response: response.url.endswith("/fragments/status")):
        page.get_by_role("button", name="Refresh status").click()
    expect(page.get_by_text("Active", exact=True)).to_be_visible()
    expect(page.get_by_role("heading", name="Overview")).to_be_visible()
    page.close()



def verify_identity(browser, base_url):
    context = browser.new_context(
        viewport={"width": 320, "height": 900}, java_script_enabled=False
    )
    page = context.new_page()
    username = "browser_" + secrets.token_hex(6)
    password = "isolated fixture password"
    page.goto(base_url + "/register")
    page.get_by_label("Username").fill(username)
    page.get_by_label("New password").fill(password)
    page.get_by_role("button", name="Create account").click()
    expect(page.get_by_role("heading", name="Save your recovery codes")).to_be_visible()
    expect(page.locator(".recovery-codes code")).to_have_count(5)
    page.get_by_role("link", name="Continue to account").click()
    expect(page.get_by_text("Signed in as " + username)).to_be_visible()
    page.get_by_role("button", name="Sign out").click()
    expect(page.get_by_role("heading", name="Sign in")).to_be_visible()
    page.get_by_label("Username").fill(username)
    page.get_by_label("Password").fill("incorrect fixture password")
    page.get_by_role("button", name="Sign in").click()
    expect(page.get_by_role("alert")).to_be_visible()
    expect(page.get_by_label("Username")).to_have_value(username)
    page.get_by_label("Password").fill(password)
    page.get_by_role("button", name="Sign in").click()
    expect(page.get_by_text("Signed in as " + username)).to_be_visible()
    context.close()



def verify_discovery(browser, base_url, width):
    context = browser.new_context(
        viewport={"width": width, "height": 900}, java_script_enabled=False
    )
    page = context.new_page()
    page.goto(base_url + "/discover")
    expect(page.get_by_label("Sort by")).to_be_visible()
    count = page.locator(".listing-grid").evaluate(
        "(grid) => getComputedStyle(grid).gridTemplateColumns.split(' ').length"
    )
    assert count == (1 if width < 768 else 2)
    page.screenshot(path=f"bin/smoke/discovery-{width}.png")
    if width < 1024:
        page.locator(".filter-drawer summary").click()
        filters = page.locator(".filter-drawer")
    else:
        filters = page.locator(".discovery-rail")
    filters.get_by_label("Availability").select_option("orderable")
    filters.get_by_role("button", name="Apply filters").click()
    expect(page.get_by_role("link", name="Cork desk mat")).to_be_visible()
    expect(page.get_by_role("link", name="Compact mechanical keyboard")).to_have_count(0)
    expect(page.get_by_label("Sort by")).to_be_visible()
    page.go_back()
    expect(page.get_by_role("link", name="Compact mechanical keyboard")).to_be_visible()
    context.close()


def main():
    base_url = os.environ["MARKETPLACE_SMOKE_URL"]
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(
            executable_path=os.getenv("MARKETPLACE_CHROMIUM_EXECUTABLE")
        )
        for width in (320, 375, 768, 1024, 1440):
            verify_layout(browser, base_url, width)
        verify_htmx(browser, base_url)
        verify_identity(browser, base_url)
        for width in (375, 1024):
            verify_discovery(browser, base_url, width)
        browser.close()


if __name__ == "__main__":
    main()
