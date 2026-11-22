"""Check the built app against the running local PostgreSQL fixture."""

import base64
import os
import re
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
    page.goto(base_url + "/discover?q=keyboard")
    save = page.get_by_role("button", name="Save Compact mechanical keyboard")
    save.click()
    expect(page).to_have_url(base_url + "/discover?q=keyboard")
    expect(page.get_by_role("button", name="Remove Compact mechanical keyboard from saved listings")).to_be_visible()
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
        summary = page.locator(".filter-drawer summary")
        summary.focus()
        page.keyboard.press("Enter")
        expect(summary).to_be_focused()
        expect(summary).to_contain_text("Close filters")
        expect(page.get_by_label("Sort by")).to_be_hidden()
        page.keyboard.press("Tab")
        expect(page.locator(".filter-drawer input[type=search]")).to_be_focused()
        page.keyboard.press("Shift+Tab")
        page.keyboard.press("Enter")
        expect(summary).to_be_focused()
        expect(page.get_by_label("Sort by")).to_be_visible()
        page.keyboard.press("Enter")
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



def verify_seller(browser, base_url):
    context = browser.new_context(
        viewport={"width": 375, "height": 900}, java_script_enabled=False
    )
    page = context.new_page()
    page.goto(base_url + "/register")
    username = "seller_" + secrets.token_hex(6)
    page.get_by_label("Username").fill(username)
    page.get_by_label("New password").fill("isolated seller fixture password")
    page.get_by_role("button", name="Create account").click()
    page.get_by_role("link", name="Continue to account").click()
    page.get_by_role("link", name="Sell", exact=True).click()
    page.get_by_role("link", name="New listing").click()
    title = "Mobile seller fixture " + username
    page.get_by_label("Title").fill(title)
    page.get_by_role("button", name="Save draft", exact=True).click()
    expect(page.get_by_role("status")).to_contain_text("Draft saved")
    expect(page.get_by_label("Title")).to_have_value(title)
    page.get_by_label("Description").fill("Digital fixture description")
    page.get_by_label("Category").select_option("templates")
    page.get_by_label("Listing type").select_option("digital")
    page.get_by_label("Price").fill("12.50")
    page.get_by_label("Stock", exact=True).fill("2")
    page.get_by_label("Digital delivery").select_option("manual")
    page.get_by_label("Fulfillment terms").fill("Manual delivery after purchase")
    page.get_by_role("button", name="Save and preview").click()
    expect(page.get_by_role("heading", name="Listing preview", exact=True)).to_be_visible()
    expect(page.get_by_role("region", name="Mobile preview")).to_contain_text(title)
    bounds = page.get_by_role("region", name="Mobile preview").bounding_box()
    assert bounds and bounds["x"] + bounds["width"] <= 375
    page.screenshot(path="bin/smoke/seller-preview-375.png")
    page.get_by_role("button", name="Publish listing").click()
    expect(page.get_by_role("heading", name=title)).to_be_visible()
    listing_url = page.url
    context.close()
    return listing_url



def verify_upload_progress(browser, base_url):
    page = browser.new_page(viewport={"width": 375, "height": 900})
    page.goto(base_url + "/register")
    page.get_by_label("Username").fill("upload_" + secrets.token_hex(6))
    page.get_by_label("New password").fill("isolated upload fixture password")
    page.get_by_role("button", name="Create account").click()
    page.goto(base_url + "/sell/new")
    page.get_by_label("Title").fill("Image upload fixture")
    file_input = page.locator("input[name=image]")
    file_input.set_input_files({
        "name": "oversize.png", "mimeType": "image/png", "buffer": bytes(3 * 1024 * 1024 + 1)
    })
    page.get_by_role("button", name="Save draft", exact=True).click()
    expect(page.locator("#upload-message")).to_contain_text("3 MB or less")
    expect(page.get_by_label("Title")).to_have_value("Image upload fixture")
    file_input.set_input_files({
        "name": "fixture.png", "mimeType": "image/png", "buffer": base64.b64decode("iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAFklEQVR4nGNMrU1jYGBgYmBgYGBgAAAPiQFMBq9DwQAAAABJRU5ErkJggg==")
    })

    page.evaluate("""() => {
        const progress = document.querySelector('#upload-progress');
        new MutationObserver(() => {
            if (!progress.hidden) sessionStorage.setItem('verified-upload-progress', 'yes');
        }).observe(progress, {attributes: true});
    }""")
    with page.expect_response(
        lambda response: response.url.endswith("/sell/new")
        and response.request.method == "POST"
    ) as submitted:
        page.get_by_role("button", name="Save draft", exact=True).click()
    assert submitted.value.status == 204, f"Upload returned {submitted.value.status}"
    expect(page).to_have_url(re.compile(r"/sell/drafts/[0-9a-f-]+\?saved=1$"))
    assert page.evaluate("sessionStorage.getItem('verified-upload-progress')") == "yes"
    expect(page.locator("img[alt='Current draft image']")).to_be_visible()
    expect(page.get_by_role("status")).to_contain_text("Draft saved")
    page.close()


def verify_messages(browser, base_url, listing_url):
    context = browser.new_context(
        viewport={"width": 375, "height": 900}, java_script_enabled=False
    )
    page = context.new_page()
    page.goto(base_url + "/register")
    page.get_by_label("Username").fill("buyer_" + secrets.token_hex(6))
    page.get_by_label("New password").fill("isolated buyer fixture password")
    page.get_by_role("button", name="Create account").click()
    page.goto(listing_url)
    page.get_by_role("button", name="Message seller").click()
    page.get_by_label("Message", exact=True).fill("Is this available for manual delivery?")
    page.get_by_role("button", name="Send message").click()
    expect(page.get_by_role("status")).to_contain_text("Message sent")
    expect(page.get_by_text("Is this available for manual delivery?")).to_be_visible()
    thread_url = page.url
    page.get_by_role("link", name="Inbox", exact=True).click()
    expect(page.get_by_role("link", name="Mobile seller fixture", exact=False)).to_be_visible()
    signed_in = context.storage_state()
    context.close()


    enhanced = browser.new_context(storage_state=signed_in)
    thread = enhanced.new_page()
    thread.clock.install()
    thread.goto(thread_url)
    with thread.expect_response(lambda response: response.url.endswith("/entries")) as polled:
        thread.clock.fast_forward(31000)
    assert polled.value.ok
    expect(thread.get_by_text("Is this available for manual delivery?")).to_be_visible()
    enhanced.close()


def register_fixture(page, base_url, prefix):
    page.goto(base_url + "/register")
    page.get_by_label("Username").fill(prefix + "_" + secrets.token_hex(6))
    page.get_by_label("New password").fill("isolated orders fixture password")
    page.get_by_role("button", name="Create account").click()


def create_physical_fixture(merchant, base_url):
    register_fixture(merchant, base_url, "seller")
    merchant.goto(base_url + "/sell/new")
    merchant.get_by_label("Title").fill("Browser physical order fixture")
    merchant.get_by_label("Description").fill("Fictional physical test item")
    merchant.get_by_label("Category").select_option("accessories")
    merchant.get_by_label("Listing type").select_option("physical")
    merchant.get_by_label("Price").fill("12.50")
    merchant.get_by_label("Stock", exact=True).fill("2")
    merchant.get_by_label("Physical condition").select_option("new")
    merchant.get_by_label("Ships to EU").check()
    merchant.get_by_label("Fulfillment terms").fill("Fictional dispatch in two days")
    merchant.get_by_role("button", name="Save and preview").click()
    merchant.get_by_role("button", name="Publish listing").click()
    expect(merchant.get_by_role("heading", name="Browser physical order fixture")).to_be_visible()
    return merchant.url


def verify_orders(browser, base_url):
    settings = {"viewport": {"width": 375, "height": 900}, "java_script_enabled": False}
    seller_context = browser.new_context(**settings)
    merchant = seller_context.new_page()
    physical = create_physical_fixture(merchant, base_url)
    buyer_context = browser.new_context(**settings, accept_downloads=True)
    buyer = buyer_context.new_page()
    register_fixture(buyer, base_url, "buyer")
    buyer.goto(physical)
    buyer.get_by_role("link", name="Simulated checkout").click()
    buyer.get_by_label("Shipping region").select_option("EU")
    buyer.get_by_label("Test shipping address").fill("Fictional test address")
    buyer.get_by_role("button", name="Create order and quote").click()
    order_url = buyer.url
    expect(buyer.get_by_role("region", name="Mock checkout")).to_be_visible()
    buyer.get_by_role("button", name="Simulate and verify payment").click()
    merchant.goto(order_url)
    expect(merchant.get_by_text("Fictional test address", exact=True)).to_be_visible()
    merchant.get_by_label("Tracking reference").fill("TEST-123")
    merchant.get_by_role("button", name="Mark dispatched").click()
    buyer.reload()
    expect(buyer.get_by_text("Tracking reference TEST-123")).to_be_visible()
    buyer.get_by_role("button", name="Confirm receipt").click()
    expect(buyer.get_by_text("Completed", exact=True).first).to_be_visible()
    for width in (320, 375, 768, 1024, 1440):
        buyer.set_viewport_size({"width": width, "height": 900})
        assert buyer.locator("html").evaluate("(root) => root.scrollWidth") <= width
    buyer.set_viewport_size(settings["viewport"])
    buyer.screenshot(path="bin/smoke/physical-order-375.png")

    buyer.goto(base_url + "/listings/3cd38877-43d7-403a-94b4-450456bde7ce")
    buyer.get_by_role("link", name="Simulated checkout").click()
    buyer.get_by_role("button", name="Create order and quote").click()
    buyer.get_by_role("button", name="Simulate and verify payment").click()
    with buyer.expect_download() as private_file:
        buyer.get_by_role("link", name="Download private file").click()
    assert "Workshop inventory fixture" in private_file.value.path().read_text()
    buyer.get_by_role("button", name="Confirm receipt").click()
    expect(buyer.get_by_text("Completed", exact=True).first).to_be_visible()
    buyer.goto(base_url + "/messages")
    buyer.get_by_role("link", name="Workshop inventory sheets").click()
    expect(buyer.get_by_text('Bought "Workshop inventory sheets"', exact=True)).to_be_visible()
    buyer_context.close()
    seller_context.close()


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
        listing_url = verify_seller(browser, base_url)
        verify_messages(browser, base_url, listing_url)
        verify_upload_progress(browser, base_url)
        verify_orders(browser, base_url)
        browser.close()


if __name__ == "__main__":
    main()
