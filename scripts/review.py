"""Exercise the real React gateway console; optional recording contains actual traffic."""

from __future__ import annotations

import argparse
import json
import signal
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

ROOT = Path(__file__).resolve().parents[1]
KEY = "synthetic-throttle-amber-key-0001"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--record", action="store_true")
    args = parser.parse_args()
    out = ROOT / "docs/results/browser"
    out.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory() as folder:
        logpath = Path(folder) / "gateway.log"
        with logpath.open("w") as log:
            process = subprocess.Popen(
                [
                    str(ROOT / "bin/throttle"),
                    "--demo",
                    "--addr",
                    "127.0.0.1:0",
                    "--web",
                    str(ROOT / "apps/web/dist"),
                ],
                stdout=log,
                stderr=log,
            )
            try:
                base = ""
                for _ in range(200):
                    for line in logpath.read_text().splitlines():
                        event = json.loads(line)
                        if event.get("msg") == "ready":
                            base = event["url"]
                    if base:
                        break
                    if process.poll() is not None:
                        raise RuntimeError(logpath.read_text())
                    time.sleep(0.025)
                if not base:
                    raise RuntimeError("readiness timed out")

                def get(path: str, token: str = "") -> tuple[int, str]:
                    request = urllib.request.Request(
                        base + path, headers={"Authorization": "Bearer " + token}
                    )
                    try:
                        with urllib.request.urlopen(request, timeout=3) as response:
                            return response.status, response.read().decode()
                    except urllib.error.HTTPError as error:
                        return error.code, error.read().decode()

                errors: list[str] = []
                with sync_playwright() as p:
                    browser = p.chromium.launch(executable_path=p.chromium.executable_path)
                    context = browser.new_context(
                        viewport={"width": 1440, "height": 1000},
                        **(
                            {
                                "record_video_dir": str(out / "recordings"),
                                "record_video_size": {"width": 1440, "height": 1000},
                            }
                            if args.record
                            else {}
                        ),
                    )
                    page = context.new_page()
                    page.on("pageerror", lambda error: errors.append(str(error)))
                    started = time.monotonic()
                    page.goto(base)
                    expect(page.locator("h1")).to_contain_text("Room for")
                    expect(
                        page.get_by_role("status").filter(has_text="Live connection")
                    ).to_be_visible()
                    async_receipts = []

                    def campaign(mode: str, algorithm: str) -> None:
                        page.get_by_role("radio", name=mode, exact=True).check()
                        page.get_by_label("Algorithm", exact=True).select_option(algorithm)
                        page.get_by_role("button", name="Apply policy").click()
                        page.wait_for_timeout(200)
                        page.get_by_role("button", name="Run synthetic traffic").click()
                        expect(page.get_by_role("button", name="Traffic in flight…")).to_be_visible(
                            timeout=3000
                        )
                        expect(
                            page.get_by_role("button", name="Run synthetic traffic")
                        ).to_be_enabled(timeout=10000)
                        current = json.loads(get("/api/state")[1])
                        result = current["campaign_result"]
                        assert result["mode"] == mode and result["algorithm"] == algorithm
                        assert result["classes"]["legitimate"]["offered"] > 0
                        async_receipts.append(
                            {"mode": mode, "algorithm": algorithm, "classes": result["classes"]}
                        )

                    campaign("fixed", "token")
                    campaign("adaptive", "token")
                    page.get_by_role("button", name="legit-amber", exact=False).click()
                    expect(page.locator(".client-inspector h3")).to_have_text("legit-amber")
                    page.screenshot(path=str(out / "desktop.png"), full_page=True)
                    campaign("adaptive", "sliding")
                    campaign("adaptive", "gcra")
                    page.get_by_role("button", name="fail", exact=True).click()
                    page.wait_for_timeout(1200)
                    faults = [get("/proxy/v1/read", KEY)[0] for _ in range(3)]
                    assert faults == [503, 503, 503], faults
                    fault_state = json.loads(get("/api/state")[1])
                    assert fault_state["breaker"] == "open"
                    page.get_by_role("button", name="healthy", exact=True).click()
                    page.wait_for_timeout(1200)
                    assert get("/proxy/v1/read", KEY)[0] == 200
                    if args.record:
                        cycle = 0
                        while time.monotonic() - started < 94:
                            campaign(
                                "fixed" if cycle % 2 else "adaptive",
                                ["token", "sliding", "gcra"][cycle % 3],
                            )
                            cycle += 1
                    widths = []
                    for width in [390, 768, 1024, 1440]:
                        page.set_viewport_size({"width": width, "height": 1000})
                        page.wait_for_timeout(200)
                        assert page.evaluate(
                            "document.documentElement.scrollWidth <= innerWidth"
                        ), width
                        page.screenshot(path=str(out / f"width-{width}.png"), full_page=True)
                        widths.append(width)
                    page.get_by_role("button", name="legit-coral", exact=False).focus()
                    page.keyboard.press("Enter")
                    expect(page.locator(".client-inspector h3")).to_have_text("legit-coral")
                    assert not errors, errors
                    video = page.video.path() if args.record else None
                    context.close()
                    browser.close()
                    if video:
                        Path(video).rename(out / "demo.webm")
                    (out / "verification.json").write_text(
                        json.dumps(
                            {
                                "real_http_gateway": True,
                                "viewports_no_overflow": widths,
                                "page_errors": errors,
                                "campaigns": async_receipts,
                                "actual_origin_fault_statuses": faults,
                                "fault_breaker": fault_state["breaker"],
                                "healthy_probe_recovered": True,
                                "keyboard_client_selection": True,
                                "video": "docs/results/browser/demo.webm" if video else None,
                            },
                            indent=2,
                        )
                        + "\n"
                    )
            finally:
                if process.poll() is None:
                    process.send_signal(signal.SIGTERM)
                    process.wait(timeout=8)
                (out / "gateway.log").write_text(logpath.read_text())
                if process.returncode != 0:
                    raise RuntimeError("gateway exited abnormally: " + logpath.read_text())


if __name__ == "__main__":
    main()
