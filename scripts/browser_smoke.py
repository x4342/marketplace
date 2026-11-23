"""Check the built app against the running local PostgreSQL fixture."""

import os
import re
import secrets
import subprocess
import struct
import zlib
from time import perf_counter
from urllib.parse import urlsplit

from playwright.sync_api import expect, sync_playwright


def assert_dark(page):
    expect(page.locator("html")).to_have_attribute("data-theme", "dark")
    expect(page.locator('meta[name="color-scheme"]')).to_have_attribute("content", "dark")
    assert page.locator("html").evaluate("element => getComputedStyle(element).colorScheme") == "dark"


def verify_layout(browser, base_url, width):
    context = browser.new_context(
        viewport={"width": width, "height": 900}, java_script_enabled=False, color_scheme="light"
    )
    page = context.new_page()
    requests = []
    page.on("request", lambda request: requests.append(request.url))

    page.goto(base_url)
    assert urlsplit(page.url).path == "/discover"
    heading = page.get_by_role("heading", name="Discover", exact=True)
    expect(heading).to_be_visible()
    page.keyboard.press("Tab")
    expect(page.get_by_role("link", name="Skip to content")).to_be_focused()
    page.keyboard.press("Enter")
    expect(page.get_by_role("main")).to_be_focused()

    assert_dark(page)
    expect(page.get_by_role("button", name="Light", exact=True)).to_have_count(0)
    expect(page.get_by_role("button", name="Dark", exact=True)).to_have_count(0)
    viewport = page.locator("html").bounding_box()
    assert viewport and viewport["width"] == width
    assert page.locator("html").evaluate("root => root.scrollWidth") == width
    expect(page.get_by_role("link", name="Overview", exact=True)).to_have_count(0)
    assert page.locator("body").evaluate("""body => {
        const context = document.createElement('canvas').getContext('2d');
        const luminance = color => {
            context.fillStyle = color;
            context.fillRect(0, 0, 1, 1);
            const rgb = Array.from(context.getImageData(0, 0, 1, 1).data.slice(0, 3));
            const linear = rgb.map(channel => {
                const value = channel / 255;
                return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
            });
            return 0.2126 * linear[0] + 0.7152 * linear[1] + 0.0722 * linear[2];
        };
        const style = getComputedStyle(body);
        const [dark, light] = [luminance(style.color), luminance(style.backgroundColor)].sort((a, b) => a - b);
        return (light + 0.05) / (dark + 0.05);
    }""") >= 4.5
    page.screenshot(path=f"bin/smoke/{width}-dark.png")

    assert all(urlsplit(url).netloc == urlsplit(base_url).netloc for url in requests)
    context.close()


def verify_platform(browser, base_url, interactive=False):
    context = browser.new_context(viewport={"width": 320, "height": 900}, java_script_enabled=interactive)
    page = context.new_page()
    response = page.goto(base_url + "/admin")
    assert urlsplit(page.url).path == "/login"
    username = register_fixture(page, base_url, "browserplatform")
    assert page.goto(base_url + "/admin").status == 403
    assert page.goto(base_url + "/staff").status == 403
    subprocess.run(
        ["psql", os.environ["DATABASE_URL"], "-X", "-v", "ON_ERROR_STOP=1", "-v", "username=" + username],
        input="INSERT INTO role_assignments(user_id,role) SELECT id,'moderator' FROM users WHERE username=:'username';",
        text=True,
        check=True,
    )
    page.goto(base_url + "/staff")
    expect(page.get_by_role("heading", name="Staff workspace")).to_be_visible()
    assert_dark(page)
    assert page.goto(base_url + "/admin/categories").status == 403
    subprocess.run(
        ["psql", os.environ["DATABASE_URL"], "-X", "-v", "ON_ERROR_STOP=1", "-v", "username=" + username],
        input="UPDATE role_assignments SET role='admin' WHERE user_id=(SELECT id FROM users WHERE username=:'username');",
        text=True,
        check=True,
    )
    page.goto(base_url + "/admin")
    expect(page.get_by_role("heading", name="Administrator dashboard")).to_be_visible()
    if interactive:
        with page.expect_response(lambda response: response.url.endswith("/admin/status")):
            page.get_by_role("button", name="Refresh status").click()
    else:
        with page.expect_navigation():
            page.get_by_role("button", name="Refresh status").click()
    expect(page.get_by_text("Last heartbeat", exact=True)).to_be_visible()
    for route, heading in (("/admin", "Administrator dashboard"), ("/admin/categories", "Product categories"),
                           ("/admin/users", "Users and roles"), ("/staff", "Staff workspace")):
        page.goto(base_url + route)
        expect(page.get_by_role("heading", name=heading, exact=True)).to_be_visible()
        for width in (320, 375, 768, 1024, 1440):
            page.set_viewport_size({"width": width, "height": 900})
            assert_dark(page)
            assert page.locator("html").evaluate("root => root.scrollWidth") == width
            if width in (320, 768, 1440) and not interactive:
                name = route.rsplit("/", 1)[-1]
                page.screenshot(path=f"bin/smoke/m13-{name}-{width}.png")
        page.keyboard.press("Tab")
        expect(page.get_by_role("link", name="Skip to content")).to_be_focused()
        page.keyboard.press("Enter")
        expect(page.get_by_role("main")).to_be_focused()
    page.goto(base_url + "/admin/categories")
    name = "Keyboard accessories " + secrets.token_hex(4)
    page.get_by_label("Name", exact=True).fill(name)
    page.get_by_label("Description").fill("Synthetic category created by the browser suite.")
    page.get_by_label("Display order").fill("17")
    page.get_by_label("Confirm category change.").check()
    page.get_by_role("button", name="Save category").click()
    expect(page.get_by_role("status").filter(has_text="Category saved.")).to_be_visible()
    page.get_by_role("link", name=name, exact=True).click()
    expect(page.get_by_label("Name", exact=True)).to_have_value(name)
    page.get_by_label("Name", exact=True).fill(name + " updated")
    if interactive:
        page.once("dialog", lambda dialog: dialog.dismiss())
        page.get_by_role("button", name="Save category").click()
        expect(page.get_by_label("Name", exact=True)).to_have_value(name + " updated")
        page.once("dialog", lambda dialog: dialog.accept())
    else:
        page.get_by_label("Confirm category change.").check()
    page.get_by_role("button", name="Save category").click()
    expect(page.get_by_role("status").filter(has_text="Category saved.")).to_be_visible()
    expect(page.get_by_role("link", name=name + " updated", exact=True)).to_be_visible()
    context.close()


def verify_identity(browser, base_url):
    context = browser.new_context(
        viewport={"width": 320, "height": 900}, java_script_enabled=False
    )
    page = context.new_page()
    username = "browser" + secrets.token_hex(6)
    password = " é" + "🦊" * 124 + "★ "
    page.goto(base_url + "/register")
    assert_dark(page)
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
    assert_dark(page)
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
    username = "seller" + secrets.token_hex(6)
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
    page.locator("input[name=image]").set_input_files(listing_photo())
    page.get_by_role("button", name="Save and preview").click()
    expect(page.get_by_role("heading", name="Listing preview", exact=True)).to_be_visible()
    expect(page.get_by_role("region", name="Mobile preview")).to_contain_text(title)
    bounds = page.get_by_role("region", name="Mobile preview").bounding_box()
    assert bounds and bounds["x"] + bounds["width"] <= 375
    page.screenshot(path="bin/smoke/seller-preview-375.png")
    page.get_by_role("button", name="Publish listing").click()
    expect(page.get_by_role("heading", name=title)).to_be_visible()
    photo = page.get_by_role("img", name=title, exact=True)
    expect(photo).to_be_visible()
    assert photo.evaluate("img => img.naturalWidth === 400 && img.naturalHeight === 400")
    listing_url = page.url
    context.close()
    return listing_url



def listing_photo():
    def chunk(name, content):
        return struct.pack(">I", len(content)) + name + content + struct.pack(">I", zlib.crc32(name + content))

    header = struct.pack(">IIBBBBB", 640, 400, 8, 2, 0, 0, 0)
    row = b"\0" + bytes((210, 25, 40)) * 320 + bytes((25, 65, 210)) * 320
    return {
        "name": "fixture.png",
        "mimeType": "image/png",
        "buffer": b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", header) +
        chunk(b"IDAT", zlib.compress(row * 400)) + chunk(b"IEND", b""),
    }


def verify_profiles(browser, base_url, interactive=False):
    context = browser.new_context(viewport={"width": 320, "height": 900}, java_script_enabled=interactive)
    page = context.new_page()
    username = register_fixture(page, base_url, "profile")
    page.goto(base_url + "/account/profile")
    page.get_by_label("Biography").fill("Public marketplace profile fixture")
    page.locator("input[name=image]").set_input_files(listing_photo())
    if interactive:
        expect(page.locator("#crop-status")).to_contain_text("Confirm")
        page.get_by_label("Zoom").press("End")
        page.get_by_role("button", name="Confirm crop").click()
        expect(page.locator("#crop-status")).to_contain_text("Crop confirmed")
    page.get_by_role("button", name="Save profile").click()
    expect(page.get_by_role("status").filter(has_text="Profile saved.")).to_be_visible()
    public = base_url + "/users/" + username
    page.goto(public)
    expect(page.get_by_text("Public marketplace profile fixture", exact=True)).to_be_visible()
    for width in (320, 375, 768, 1024, 1440):
        page.set_viewport_size({"width": width, "height": 900})
        assert page.locator("html").evaluate("node => node.scrollWidth") == width
        assert_dark(page)
    avatar = page.locator(".profile-header .profile-avatar")
    expect(avatar).to_be_visible()
    assert avatar.evaluate("img => img.naturalWidth === 256 && img.naturalHeight === 256")
    context.clear_cookies()
    page.reload()
    expect(page.get_by_role("heading", name=username)).to_be_visible()
    avatar_url = avatar.get_attribute("src")
    page.goto(base_url + "/users/workshop")
    expect(page.get_by_role("link", name="Visit storefront")).to_be_visible()
    page.get_by_role("link", name="Contact seller about Compact mechanical keyboard").click()
    expect(page.locator("#listing-contact")).to_have_text("Sign in to message the seller")
    context.close()
    return base_url + avatar_url


def verify_upload_progress(browser, base_url):
    page = browser.new_page(viewport={"width": 375, "height": 900}, has_touch=True)
    page.goto(base_url + "/register")
    page.get_by_label("Username").fill("upload" + secrets.token_hex(6))
    page.get_by_label("New password").fill("isolated upload fixture password")
    page.get_by_role("button", name="Create account").click()
    page.goto(base_url + "/sell/new")
    assert_dark(page)
    page.get_by_label("Title").fill("Image upload fixture")
    file_input = page.locator("input[name=image]")
    file_input.set_input_files({
        "name": "oversize.png", "mimeType": "image/png", "buffer": bytes(10_000_001)
    })
    expect(page.locator("#upload-message")).to_contain_text("10 MB or less")
    expect(page.get_by_label("Title")).to_have_value("Image upload fixture")
    file_input.set_input_files(listing_photo())
    crop_status = page.locator("#crop-status")
    expect(crop_status).to_contain_text("Confirm the crop")
    page.get_by_role("button", name="Cancel image").click()
    expect(page.locator("#image-cropper")).to_be_hidden()
    file_input.set_input_files(listing_photo())
    expect(crop_status).to_contain_text("Confirm the crop")
    preview = page.get_by_role("img", name="Square crop preview")
    for width in (320, 375, 768, 1024, 1440):
        page.set_viewport_size({"width": width, "height": 900})
        area = preview.bounding_box()
        assert area and abs(area["width"] - area["height"]) < 1
        assert page.locator("html").evaluate("node => node.scrollWidth") == width
    page.set_viewport_size({"width": 375, "height": 900})
    page.get_by_label("Zoom").press("End")
    preview.scroll_into_view_if_needed()
    area = preview.bounding_box()
    x, y = area["x"] + area["width"] / 2, area["y"] + area["height"] / 2
    horizontal = page.get_by_label("Horizontal position")
    previous = int(horizontal.input_value())
    page.mouse.move(x, y)
    page.mouse.down()
    page.mouse.move(x - 50, y, steps=5)
    page.mouse.up()
    assert int(horizontal.input_value()) > previous
    previous = int(horizontal.input_value())
    touch = page.context.new_cdp_session(page)
    touch.send("Input.dispatchTouchEvent", {"type": "touchStart", "touchPoints": [{"x": x, "y": y}]})
    touch.send("Input.dispatchTouchEvent", {"type": "touchMove", "touchPoints": [{"x": x - 45, "y": y}]})
    touch.send("Input.dispatchTouchEvent", {"type": "touchEnd", "touchPoints": []})
    assert int(horizontal.input_value()) > previous
    horizontal.press("End")
    page.get_by_role("button", name="Confirm crop").tap()
    expect(crop_status).to_contain_text("Crop confirmed")
    assert preview.evaluate("""canvas => {
        const [red, , blue] = canvas.getContext('2d').getImageData(256, 256, 1, 1).data;
        return blue > red + 100;
    }""")
    page.screenshot(path="bin/smoke/crop-375.png")

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
    stored = page.locator("img[alt='Current draft image']")
    expect(stored).to_be_visible()
    assert stored.evaluate("img => img.naturalWidth === 256 && img.naturalHeight === 256")
    expect(page.get_by_role("status")).to_contain_text("Draft saved")
    page.close()


def verify_messages(browser, base_url, listing_url):
    context = browser.new_context(
        viewport={"width": 375, "height": 900}, java_script_enabled=False
    )
    page = context.new_page()
    page.goto(base_url + "/register")
    page.get_by_label("Username").fill("buyer" + secrets.token_hex(6))
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
    username = prefix[:8] + secrets.token_hex(6)
    page.get_by_label("Username").fill(username)
    page.get_by_label("New password").fill("isolated orders fixture password")
    page.get_by_role("button", name="Create account").click()
    return username


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
    assert_dark(buyer)
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

    buyer.get_by_label("Rating", exact=True).select_option("5")
    buyer.get_by_label("Written review").fill("Item arrived as described.")
    buyer.get_by_role("button", name="Submit review").click()
    expect(buyer.get_by_role("status")).to_contain_text("Review submitted")
    buyer.get_by_role("link", name="Open your review").click()
    expect(buyer.get_by_text("Visible on the listing", exact=True)).to_be_visible()
    buyer.goto(physical)
    expect(buyer.get_by_text("Item arrived as described.", exact=True)).to_be_visible()
    expect(buyer.get_by_text("Verified purchase · Browser physical order fixture", exact=True)).to_be_visible()
    buyer.screenshot(path="bin/smoke/verified-review-375.png")
    merchant.goto(base_url + "/sell/insights")
    expect(merchant.get_by_role("heading", name="Seller insights")).to_be_visible()
    expect(merchant.get_by_text("USD 12.50", exact=True)).to_be_visible()

    merchant.goto(base_url + "/earnings")
    assert_dark(merchant)
    balance = merchant.get_by_role("region", name="USD balance")
    expect(balance.get_by_text("USD 11.87", exact=True)).to_be_visible()
    for width in (320, 375, 768, 1024, 1440):
        merchant.set_viewport_size({"width": width, "height": 900})
        assert merchant.locator("html").evaluate("(root) => root.scrollWidth") <= width
    merchant.set_viewport_size(settings["viewport"])
    merchant.screenshot(path="bin/smoke/seller-earnings-375.png")
    merchant.get_by_label("Amount", exact=True).fill("5.00")
    merchant.get_by_role("button", name="Request payout").click()
    expect(merchant.get_by_text("Reserved", exact=True).first).to_be_visible()
    expect(merchant.get_by_text("50000 TEST units · simulation network")).to_be_visible()

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


def verify_disputes(browser, base_url):
    context = browser.new_context(viewport={"width": 375, "height": 900}, java_script_enabled=False)
    buyer = context.new_page()
    register_fixture(buyer, base_url, "disputebuyer")
    buyer.goto(base_url + "/listings/3cd38877-43d7-403a-94b4-450456bde7ce")
    buyer.get_by_role("link", name="Simulated checkout").click()
    buyer.get_by_role("button", name="Create order and quote").click()
    buyer.get_by_role("button", name="Simulate and verify payment").click()
    buyer.get_by_text("Report a fulfillment problem", exact=True).click()
    buyer.get_by_label("Complaint category").select_option("delivery_failed")
    buyer.get_by_label("Describe the problem").fill("Synthetic fulfillment issue")
    buyer.get_by_role("button", name="Open dispute").click()
    buyer.get_by_role("link", name="Open dispute").click()
    expect(buyer.get_by_role("region", name="Case history")).to_contain_text("Synthetic fulfillment issue")
    buyer.get_by_label("Submission type").select_option("evidence")
    buyer.get_by_role("textbox", name="Submission", exact=True).fill("Synthetic browser evidence")
    buyer.get_by_role("button", name="Submit to case").click()
    expect(buyer.get_by_role("status")).to_contain_text("Dispute updated")
    expect(buyer.get_by_role("region", name="Restricted evidence")).to_contain_text("Synthetic browser evidence")
    for width in (320, 375, 768, 1024, 1440):
        buyer.set_viewport_size({"width": width, "height": 900})
        assert buyer.locator("html").evaluate("(root) => root.scrollWidth") <= width
    buyer.set_viewport_size({"width": 375, "height": 900})
    buyer.screenshot(path="bin/smoke/dispute-375.png")
    case_path = buyer.url.split("?", 1)[0].removeprefix(base_url)

    buyer.goto(base_url + "/account")
    buyer.get_by_role("button", name="Sign out").click()
    username = register_fixture(buyer, base_url, "browsermod")
    subprocess.run(
        ["psql", os.environ["DATABASE_URL"], "-X", "-v", "ON_ERROR_STOP=1", "-v", "username=" + username],
        input="INSERT INTO role_assignments(user_id,role) SELECT id,'moderator' FROM users WHERE username=:'username';",
        text=True,
        check=True,
    )
    buyer.goto(base_url + "/admin/disputes?state=open")
    assert_dark(buyer)
    record = buyer.locator(f'a[href="/admin{case_path}"]')
    expect(record).to_be_visible()
    for width in (320, 375, 768, 1024, 1440):
        buyer.set_viewport_size({"width": width, "height": 900})
        assert buyer.locator("html").evaluate("(root) => root.scrollWidth") <= width
        label = buyer.get_by_role("table").locator(".admin-column-label", has_text="Category").first
        if width < 768:
            expect(label).to_be_visible()
            assert record.bounding_box()["height"] >= 44
        else:
            expect(label).to_be_hidden()
        if width in (375, 1440):
            buyer.screenshot(path=f"bin/smoke/admin-disputes-{width}.png", full_page=True)
    record.click()
    expect(buyer.get_by_role("region", name="Restricted evidence")).to_contain_text("Synthetic browser evidence")
    buyer.get_by_label("Decision reason").fill("Sandbox refund verified in browser")
    buyer.get_by_label("Current password").fill("isolated orders fixture password")
    buyer.get_by_role("button", name="Confirm simulated refund").click()
    expect(buyer.get_by_role("region", name="Case summary")).to_contain_text("Status: refunded")
    context.close()


def verify_slow_no_script(browser, base_url):
    context = browser.new_context(
        viewport={"width": 320, "height": 900}, java_script_enabled=False, reduced_motion="reduce"
    )
    page = context.new_page()
    connection = context.new_cdp_session(page)
    connection.send("Network.enable")
    connection.send(
        "Network.emulateNetworkConditions",
        {
            "offline": False,
            "latency": 200,
            "downloadThroughput": 200_000,
            "uploadThroughput": 93_750,
        },
    )
    requests = []
    page.on("request", lambda request: requests.append(request.url))
    started = perf_counter()
    response = page.goto(base_url + "/discover?q=keyboard")
    expect(page.get_by_label("Sort by")).to_be_visible()
    elapsed = perf_counter() - started
    assert elapsed < 10, f"discovery exceeded isolated 10-second budget: {elapsed:.2f}s"
    assert response.headers["cache-control"] == "no-store"
    assert page.locator("html").evaluate("root => root.scrollWidth") == 320
    assert page.evaluate("matchMedia('(prefers-reduced-motion: reduce)').matches")
    assert page.locator("body").evaluate(
        "body => parseFloat(getComputedStyle(body).transitionDuration) <= 0.001"
    )

    page.keyboard.press("Tab")
    skip = page.get_by_role("link", name="Skip to content")
    expect(skip).to_be_focused()
    assert skip.evaluate("link => parseFloat(getComputedStyle(link).outlineWidth)") >= 2
    page.keyboard.press("Enter")
    expect(page.get_by_role("main")).to_be_focused()

    register_fixture(page, base_url, "slowbuyer")
    page.goto(base_url + "/listings/3cd38877-43d7-403a-94b4-450456bde7ce")
    page.get_by_role("link", name="Simulated checkout").click()
    page.get_by_role("button", name="Create order and quote").click()
    page.get_by_role("button", name="Simulate and verify payment").click()
    refreshed = page.reload()
    assert refreshed.headers["cache-control"] == "no-store"
    expect(page.get_by_role("link", name="Download private file")).to_be_visible()
    assert all(urlsplit(url).netloc == urlsplit(base_url).netloc for url in requests)
    print(f"Throttled no-JavaScript discovery: {elapsed:.2f}s. Registration and simulated checkout passed.")
    page.screenshot(path="bin/smoke/slow-no-script-order-320.png")
    context.close()


def main():
    base_url = os.environ["MARKETPLACE_SMOKE_URL"]
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(
            executable_path=os.getenv("MARKETPLACE_CHROMIUM_EXECUTABLE")
        )
        for width in (320, 375, 768, 1024, 1440):
            verify_layout(browser, base_url, width)
        verify_platform(browser, base_url)
        verify_platform(browser, base_url, interactive=True)
        verify_identity(browser, base_url)
        for width in (375, 1024):
            verify_discovery(browser, base_url, width)
        listing_url = verify_seller(browser, base_url)
        verify_messages(browser, base_url, listing_url)
        verify_profiles(browser, base_url)
        verify_profiles(browser, base_url, interactive=True)
        verify_upload_progress(browser, base_url)
        verify_orders(browser, base_url)
        verify_disputes(browser, base_url)
        verify_slow_no_script(browser, base_url)
        browser.close()


if __name__ == "__main__":
    main()
