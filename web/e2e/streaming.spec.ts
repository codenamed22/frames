import { expect, test, type Page } from "@playwright/test";
import { formatBytes, formatTime } from "../src/media";

async function openPlayer(page: Page, mode: "DASH" | "HLS") {
  await page.goto("/watch");
  await page.getByRole("radio", { name: mode, exact: true }).check();
  await expect(page.locator(".playing-protocol")).toContainText(mode);
  await expect(
    page.getByRole("button", { name: "Play video", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Play video", exact: true }).click();
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video: HTMLVideoElement) => video.currentTime),
    )
    .toBeGreaterThan(2);
}

async function noOverflow(page: Page) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  const collisions = await page
    .locator("button, h1, h2, select")
    .evaluateAll((elements) =>
      elements
        .filter(
          (element) =>
            element.clientWidth > 0 &&
            element.scrollWidth > element.clientWidth + 1,
        )
        .map((element) => element.textContent),
    );
  expect(collisions).toEqual([]);
}

test("Auto chooses DASH on Chrome even when native HLS is available", async ({
  page,
}) => {
  await page.goto("/watch");
  await expect(page.locator(".playing-protocol")).toHaveText("DASH / Shaka");
  await expect(
    page.getByRole("button", { name: "Play video", exact: true }),
  ).toBeVisible();
});

test("media-source failure gives browser guidance and retry recovers", async ({
  page,
}) => {
  await page.route(
    "**/init-stream0.m4s",
    (route) =>
      route.fulfill({
        contentType: "video/mp4",
        body: Buffer.from([0, 0, 0, 4, 102, 116, 121, 112]),
      }),
    { times: 1 },
  );
  await page.goto("/watch");
  await expect(page.getByRole("alert")).toContainText("code 3014");
  await expect(page.getByRole("alert")).toContainText("Chrome or Safari");
  await expect(page.getByRole("alert")).not.toContainText("still available");
  await page.getByRole("button", { name: "Retry playback" }).click();
  await page.getByRole("button", { name: "Play video", exact: true }).click();
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video: HTMLVideoElement) => video.currentTime),
    )
    .toBeGreaterThan(2);
  await expect(page.getByRole("alert")).not.toBeVisible();
});

test("formatting handles time boundaries and missing values", () => {
  expect(formatTime(3661)).toBe("1:01:01");
  expect(formatTime(59.9)).toBe("0:59");
  expect(formatTime(Number.NaN)).toBe("0:00");
  expect(formatBytes(1_500_000)).toBe("1.5 MB");
  expect(formatBytes(2_000_000_000)).toBe("2.00 GB");
});

test("library renders its real poster, search, and manifests", async ({
  page,
}, testInfo) => {
  const failures: string[] = [];
  page.on("pageerror", (error) => failures.push(error.message));
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Your collection." }),
  ).toBeVisible();
  await expect
    .poll(() =>
      page
        .locator(".poster")
        .evaluate((image: HTMLImageElement) => image.naturalWidth),
    )
    .toBeGreaterThan(0);
  await expect(page.getByRole("link", { name: "DASH MPD" })).toHaveAttribute(
    "href",
    /manifest\.mpd$/,
  );
  await noOverflow(page);
  await page.screenshot({
    path: testInfo.outputPath("library.png"),
    fullPage: true,
  });
  await page.getByRole("searchbox").fill("definitely not this video");
  await expect(
    page.getByRole("heading", { name: "No matching videos" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Clear search" }).click();
  await expect(
    page.getByRole("button", { name: "Watch now", exact: true }),
  ).toBeVisible();
  expect(failures).toEqual([]);
});

test("DASH decodes, seeks, resumes, changes speed and reaches end", async ({
  page,
}, testInfo) => {
  const requests: string[] = [];
  const failures: string[] = [];
  page.on("request", (request) => requests.push(request.url()));
  page.on("pageerror", (error) => failures.push(error.message));
  await openPlayer(page, "DASH");
  await page.locator("video").evaluate((video: HTMLVideoElement) => {
    video.currentTime = 18;
  });
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video: HTMLVideoElement) => video.currentTime),
    )
    .toBeGreaterThan(20);
  await expect
    .poll(() => page.getByTestId("decoded-frames").innerText().then(Number))
    .toBeGreaterThan(10);
  const pixels = await page
    .locator("video")
    .evaluate((video: HTMLVideoElement) => {
      const canvas = document.createElement("canvas");
      canvas.width = 80;
      canvas.height = 45;
      const context = canvas.getContext("2d")!;
      context.drawImage(video, 0, 0, 80, 45);
      const data = context.getImageData(0, 0, 80, 45).data;
      let nonblack = 0;
      for (let index = 0; index < data.length; index += 4)
        if (data[index] + data[index + 1] + data[index + 2] > 50) nonblack++;
      return nonblack;
    });
  expect(pixels).toBeGreaterThan(80);
  await page
    .getByRole("combobox", { name: "Playback speed" })
    .selectOption("1.5");
  expect(
    await page
      .locator("video")
      .evaluate((video: HTMLVideoElement) => video.playbackRate),
  ).toBe(1.5);
  await noOverflow(page);
  await page.screenshot({
    path: testInfo.outputPath("watch.png"),
    fullPage: true,
  });
  await page
    .locator("video")
    .evaluate((video: HTMLVideoElement) => video.pause());
  await page.getByRole("button", { name: "Back to library" }).click();
  await expect(
    page.getByRole("button", { name: "Resume", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Resume", exact: true }).click();
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video: HTMLVideoElement) => video.currentTime),
    )
    .toBeGreaterThan(18);
  await page.getByRole("button", { name: "Play video", exact: true }).click();
  await page.locator("video").evaluate((video: HTMLVideoElement) => {
    video.currentTime = video.duration - 0.5;
  });
  await expect
    .poll(() =>
      page.locator("video").evaluate((video: HTMLVideoElement) => video.ended),
    )
    .toBe(true);
  await page.getByRole("button", { name: "Back to library" }).click();
  await expect(
    page.getByRole("button", { name: "Watch now", exact: true }),
  ).toBeVisible();
  expect(requests.some((url) => url.endsWith("manifest.mpd"))).toBe(true);
  expect(requests.filter((url) => url.endsWith(".m4s")).length).toBeGreaterThan(
    3,
  );
  expect(failures).toEqual([]);
});

test("DASH switches quality manually without restarting playback", async ({
  page,
}) => {
  const requests: string[] = [];
  page.on("request", (request) => requests.push(request.url()));
  await openPlayer(page, "DASH");
  const quality = page.getByRole("combobox", { name: "Video quality" });
  await expect(quality).toBeEnabled();
  await expect(quality.locator("option")).toHaveText(["Auto", "360p", "720p"]);

  let previousTime = await page
    .locator("video")
    .evaluate((video: HTMLVideoElement) => video.currentTime);
  await quality.selectOption({ label: "360p" });
  await expect(page.getByTestId("quality-mode")).toHaveText("Manual");
  await expect(page.getByTestId("active-quality")).toContainText("360p");
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video: HTMLVideoElement) => video.currentTime),
    )
    .toBeGreaterThan(previousTime);

  previousTime = await page
    .locator("video")
    .evaluate((video: HTMLVideoElement) => video.currentTime);
  await quality.selectOption({ label: "720p" });
  await expect(page.getByTestId("active-quality")).toContainText("720p");
  await expect
    .poll(() => requests.some((url) => url.includes("chunk-stream1-")))
    .toBe(true);
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video: HTMLVideoElement) => video.currentTime),
    )
    .toBeGreaterThan(previousTime);
  await expect(page.getByTestId("quality-history")).toContainText("Manual");

  await quality.selectOption("auto");
  await expect(page.getByTestId("quality-mode")).toHaveText("Auto (ABR)");
});

test("Auto ABR changes rendition with measured bandwidth", async ({
  page,
  context,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop-chrome",
    "CDP throttle is covered once",
  );
  const session = await context.newCDPSession(page);
  await session.send("Network.enable");
  await session.send("Network.emulateNetworkConditions", {
    offline: false,
    latency: 5,
    downloadThroughput: 5_000_000,
    uploadThroughput: 5_000_000,
    connectionType: "wifi",
  });
  const requests: string[] = [];
  page.on("request", (request) => requests.push(request.url()));

  await openPlayer(page, "DASH");
  await expect(page.getByTestId("quality-mode")).toHaveText("Auto (ABR)");
  await expect
    .poll(() => requests.some((url) => url.includes("chunk-stream0-")))
    .toBe(true);
  await expect
    .poll(() => requests.some((url) => url.includes("chunk-stream1-")), {
      timeout: 15_000,
    })
    .toBe(true);
  await expect(page.getByTestId("active-quality")).toContainText("720p");

  const lowRequests = requests.filter((url) =>
    url.includes("chunk-stream0-"),
  ).length;
  await session.send("Network.emulateNetworkConditions", {
    offline: false,
    latency: 30,
    downloadThroughput: 200_000,
    uploadThroughput: 200_000,
    connectionType: "cellular3g",
  });
  await page.locator("video").evaluate((video: HTMLVideoElement) => {
    video.currentTime = 22;
  });
  await expect
    .poll(
      () => requests.filter((url) => url.includes("chunk-stream0-")).length,
      { timeout: 25_000 },
    )
    .toBeGreaterThan(lowRequests);
  await expect(page.getByTestId("active-quality")).toContainText("360p");
  await expect(page.getByTestId("quality-history")).toContainText("Auto");
  await expect(page.getByTestId("last-segment")).not.toHaveText("Pending");
  await session.detach();
});

test("HLS plays and can switch to DASH without losing its position", async ({
  page,
}) => {
  await openPlayer(page, "HLS");
  await expect(
    page.getByRole("combobox", { name: "Video quality" }),
  ).toBeDisabled();
  await expect(
    page.getByRole("combobox", { name: "Video quality" }).locator("option"),
  ).toHaveText(["Automatic"]);
  await page.locator("video").evaluate((video: HTMLVideoElement) => {
    video.currentTime = 12;
    video.pause();
  });
  await page.getByRole("radio", { name: "DASH", exact: true }).check();
  await expect(page.locator(".playing-protocol")).toContainText("DASH");
  await expect(
    page.getByRole("button", { name: "Play video", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("combobox", { name: "Video quality" }),
  ).toBeEnabled();
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video: HTMLVideoElement) => video.currentTime),
    )
    .toBeGreaterThanOrEqual(11);
  await page.getByRole("button", { name: "Play video", exact: true }).click();
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video: HTMLVideoElement) => video.currentTime),
    )
    .toBeGreaterThan(14);
  await page
    .getByRole("button", { name: "Stream statistics", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Stream statistics", exact: true }),
  ).not.toBeVisible();
});

test("library failure is recoverable and long titles fit at 320px", async ({
  page,
}) => {
  await page.route(
    "**/api/video",
    (route) => route.fulfill({ status: 503, body: "unavailable" }),
    { times: 1 },
  );
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Server unavailable" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Your collection." }),
  ).toBeVisible();
  await page.route("**/api/video", async (route) => {
    const response = await route.fetch();
    await route.fulfill({
      response,
      json: {
        ...(await response.json()),
        title:
          "A_very_long_filename_with_no_spaces_for_a_local_media_collection_1080p",
      },
    });
  });
  await page.setViewportSize({ width: 320, height: 740 });
  await page.reload();
  await expect(
    page.getByRole("button", { name: "Watch now", exact: true }),
  ).toBeVisible();
  await noOverflow(page);
  await page.getByRole("button", { name: "Watch now", exact: true }).click();
  await expect(page.locator("video")).toBeVisible();
  await noOverflow(page);
});
