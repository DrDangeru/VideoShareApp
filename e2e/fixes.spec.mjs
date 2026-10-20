import { randomUUID } from 'node:crypto';
import { expect, test } from '@playwright/test';

const baseURL = 'http://127.0.0.1:18765';
const fixturePassword = 'E2e-fixture-password-42';
const appURL = (pathname) => {
  const resolved = new URL(pathname, baseURL);
  if (resolved.origin !== baseURL) throw new Error('E2E requests must remain on the fixture origin');
  return resolved.toString();
};

async function blockNonLocalRequests(context) {
  await context.route('**/*', async (route) => {
    const requested = new URL(route.request().url());
    if (requested.origin === baseURL) {
      await route.continue();
    } else {
      await route.abort();
    }
  });
}

async function loadFixtures(request) {
  const response = await request.get(appURL('/__e2e/ready'), { maxRedirects: 0 });
  expect(response.status()).toBe(200);
  const fixtures = await response.json();
  expect(fixtures.ready).toBe(true);
  return fixtures;
}

async function login(page, role) {
  await page.goto(appURL('/login'));
  await page.getByLabel('Email').fill(`${role}@e2e.test`);
  await page.getByLabel('Password').fill(fixturePassword);
  await page.getByRole('button', { name: 'Login' }).click();
  await expect(page).toHaveURL(appURL('/'));
  await expect(page.locator('.nav-text')).toContainText(`${role}@e2e.test`);
}

async function assertConfiguredContext(page, contextOptions) {
  const actual = await page.evaluate(() => ({
    width: window.innerWidth,
    height: window.innerHeight,
    userAgent: navigator.userAgent,
  }));
  expect(actual.width).toBe(contextOptions.viewport.width);
  expect(actual.height).toBe(contextOptions.viewport.height);
  if (contextOptions.isMobile) expect(actual.userAgent).toMatch(/Android/i);
}

async function createRolePage(browser, role, contextOptions) {
  const context = await browser.newContext({ ...contextOptions, serviceWorkers: 'block' });
  try {
    await blockNonLocalRequests(context);
    const page = await context.newPage();
    await login(page, role);
    await assertConfiguredContext(page, contextOptions);
    return { context, page };
  } catch (error) {
    await context.close();
    throw error;
  }
}

async function getStatus(request, pathname) {
  return request.get(appURL(pathname), { maxRedirects: 0 });
}

async function postComment(request, videoID, content) {
  return request.post(appURL('/add-comment'), {
    form: { video_id: String(videoID), content },
    maxRedirects: 0,
  });
}

async function waitForPrerollDismissal(page) {
  await expect(page.locator('#preroll-ad-overlay')).toBeHidden({ timeout: 8000 });
}

async function expectResponsiveLayout(page, includeComments = false) {
  const layout = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
    navRight: document.querySelector('.nav').getBoundingClientRect().right,
    commentsWidth: document.querySelector('.comments-section')?.getBoundingClientRect().width,
  }));
  expect(layout.scrollWidth).toBeLessThanOrEqual(layout.clientWidth + 1);
  expect(layout.navRight).toBeLessThanOrEqual(layout.clientWidth + 1);
  if (includeComments) {
    expect(layout.commentsWidth).toBeLessThanOrEqual(layout.clientWidth + 1);
  }
}

async function expectThumbnailBounds(page) {
  await expectResponsiveLayout(page);
  const bounds = await page.evaluate(() => {
    const viewportWidth = document.documentElement.clientWidth;
    const selectors = ['#thumb-section', '#btn-capture', '#thumb-preview'];
    return {
      viewportWidth,
      elements: selectors.map((selector) => {
        const rect = document.querySelector(selector).getBoundingClientRect();
        return { selector, left: rect.left, right: rect.right, width: rect.width };
      }),
    };
  });
  for (const element of bounds.elements) {
    expect(element.left, `${element.selector} left`).toBeGreaterThanOrEqual(-1);
    expect(element.right, `${element.selector} right`).toBeLessThanOrEqual(bounds.viewportWidth + 1);
  }
}

async function installThumbnailPreviewSimulation(page, thumbnailDataURL) {
  await page.locator('#vid-preview').evaluate((video, staticDataURL) => {
    let syntheticSource = '';
    let syntheticTime = 0;
    Object.defineProperties(video, {
      src: {
        configurable: true,
        get: () => syntheticSource,
        set: (value) => { syntheticSource = String(value); },
      },
      videoWidth: { configurable: true, get: () => window.thumbnailVideoWidth },
      videoHeight: { configurable: true, get: () => window.thumbnailVideoHeight },
      readyState: { configurable: true, get: () => HTMLMediaElement.HAVE_CURRENT_DATA },
      duration: { configurable: true, get: () => 0.5 },
      currentTime: {
        configurable: true,
        get: () => syntheticTime,
        set: (value) => {
          syntheticTime = value;
          queueMicrotask(() => video.dispatchEvent(new Event('seeked')));
        },
      },
      load: { configurable: true, value: () => {} },
      pause: { configurable: true, value: () => {} },
    });
    const removeAttribute = video.removeAttribute.bind(video);
    video.removeAttribute = (name) => {
      if (name.toLowerCase() === 'src') syntheticSource = '';
      removeAttribute(name);
    };
    const createElement = document.createElement.bind(document);
    window.thumbnailEncodeCalls = 0;
    window.thumbnailVideoWidth = 640;
    window.thumbnailVideoHeight = 360;
    window.thumbnailCanvasContextAvailable = true;
    window.thumbnailToDataURLThrows = false;
    window.revokedThumbnailURLs = [];
    const revokeObjectURL = URL.revokeObjectURL.bind(URL);
    URL.revokeObjectURL = (url) => {
      window.revokedThumbnailURLs.push(url);
      revokeObjectURL(url);
    };
    document.createElement = (name, ...args) => {
      const element = createElement(name, ...args);
      if (String(name).toLowerCase() === 'canvas') {
        element.getContext = (kind) => kind === '2d' && window.thumbnailCanvasContextAvailable
          ? { fillStyle: '', fillRect() {}, drawImage() {} }
          : null;
        element.toDataURL = (type) => {
          if (type !== 'image/jpeg') throw new Error('unexpected thumbnail MIME type');
          window.thumbnailEncodeCalls++;
          if (window.thumbnailToDataURLThrows) throw new Error('synthetic encoding failure');
          return staticDataURL;
        };
      }
      return element;
    };
  }, thumbnailDataURL);
}

test.beforeEach(async ({ context }) => {
  await blockNonLocalRequests(context);
});

test('uploading through the dashboard publishes and serves uploaded bytes', async ({
  browser,
  contextOptions,
  page,
}) => {
  await page.goto(appURL('/'));
  await expect(page.getByRole('heading', { name: 'Your video feed' })).toBeVisible();
  await assertConfiguredContext(page, contextOptions);
  await expectResponsiveLayout(page);
  await login(page, 'owner');
  await page.getByRole('link', { name: 'Upload your videos' }).click();

  await expect(page).toHaveURL(/\/dashboard$/);
  await expectResponsiveLayout(page);
  const suffix = randomUUID();
  const title = `E2E Uploaded ${suffix}`;
  const marker = `SYNTHETIC-E2E-UPLOAD-${suffix}`;
  await page.getByLabel('Or create a new channel').fill(`E2E Channel ${suffix}`);
  await page.getByLabel('Video File (MP4, WebM, MOV)').setInputFiles({
    name: 'fixture.mp4',
    mimeType: 'video/mp4',
    buffer: Buffer.from(marker),
  });
  await page.getByLabel('Title').fill(title);
  await page.getByLabel('Description').fill('Synthetic multipart upload fixture');
  await expect(page.getByLabel('Make video public')).toBeChecked();
  await page.getByRole('button', { name: 'Upload Video' }).click();

  await expect(page).toHaveURL(/\/dashboard$/);
  const titleLink = page.locator('a.video-title-link').filter({ hasText: title });
  await expect(titleLink).toBeVisible();
  await titleLink.click();
  await expect(page).toHaveURL(/\/watch\/\d+$/);
  await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible();
  await waitForPrerollDismissal(page);
  await expectResponsiveLayout(page, true);

  const mediaURL = await page.locator('#main-video source').getAttribute('src');
  expect(mediaURL).toBeTruthy();
  const ownerResponse = await page.request.get(appURL(mediaURL), { maxRedirects: 0 });
  expect(ownerResponse.status()).toBe(200);
  expect((await ownerResponse.body()).toString()).toBe(marker);

  const comment = `E2E uploaded comment ${suffix}`;
  await page.getByPlaceholder('Add a comment...').fill(comment);
  await page.getByRole('button', { name: 'Comment' }).click();
  await expect(page.getByText(comment, { exact: true })).toBeVisible();

  const anonymousContext = await browser.newContext({ ...contextOptions, serviceWorkers: 'block' });
  try {
    await blockNonLocalRequests(anonymousContext);
    const anonymousResponse = await anonymousContext.request.get(appURL(mediaURL), { maxRedirects: 0 });
    expect(anonymousResponse.status()).toBe(200);
    expect((await anonymousResponse.body()).toString()).toBe(marker);
  } finally {
    await anonymousContext.close();
  }
});

test('dashboard thumbnail preview resets across selections with simulated decoder metadata', async ({
  page,
  request,
  contextOptions,
}) => {
  const fixtures = await loadFixtures(request);
  await page.goto(appURL('/'));
  await assertConfiguredContext(page, contextOptions);
  await login(page, 'owner');
  await page.getByRole('link', { name: 'Upload your videos' }).click();
  await expect(page).toHaveURL(/\/dashboard$/);
  await expectResponsiveLayout(page);
  await installThumbnailPreviewSimulation(page, fixtures.thumbnailDataURL);

  const video = page.locator('#vid-preview');
  const fileInput = page.getByLabel('Video File (MP4, WebM, MOV)');
  const captureButton = page.locator('#btn-capture');
  const thumbnailData = page.locator('#thumbnail-data');
  const thumbnailPreview = page.locator('#thumb-preview');
  const thumbSection = page.locator('#thumb-section');
  await expect(captureButton).toBeDisabled();
  await expect(thumbnailData).toHaveValue('');
  await expect(thumbnailPreview).toBeHidden();

  await fileInput.setInputFiles({
    name: 'first-synthetic.mp4',
    mimeType: 'video/mp4',
    buffer: Buffer.from('SYNTHETIC-INVALID-VIDEO-FIXTURE'),
  });
  await video.evaluate((element) => element.dispatchEvent(new Event('loadeddata')));
  await expect(thumbnailData).toHaveValue(fixtures.thumbnailDataURL);
  await expect(thumbnailPreview).toBeVisible();
  await expect(captureButton).toBeEnabled();
  await expect.poll(() => thumbnailPreview.evaluate((image) => image.naturalWidth)).toBe(640);
  await expect.poll(() => thumbnailPreview.evaluate((image) => image.complete)).toBe(true);
  await expectThumbnailBounds(page);

  await fileInput.setInputFiles({
    name: 'second-synthetic.mp4',
    mimeType: 'video/mp4',
    buffer: Buffer.from('SYNTHETIC-SECOND-INVALID-VIDEO-FIXTURE'),
  });
  await expect(thumbnailData).toHaveValue('');
  await expect(thumbnailPreview).toBeHidden();
  await expect(captureButton).toBeDisabled();
  await video.evaluate((element) => element.dispatchEvent(new Event('loadeddata')));
  await expect(thumbnailData).toHaveValue(fixtures.thumbnailDataURL);
  await expect.poll(() => thumbnailPreview.evaluate((image) => image.naturalWidth)).toBe(640);
  await expect.poll(() => thumbnailPreview.evaluate((image) => image.complete)).toBe(true);
  await expectThumbnailBounds(page);
  expect(await page.evaluate(() => window.thumbnailEncodeCalls)).toBe(2);
  expect(await page.evaluate(() => window.revokedThumbnailURLs.length)).toBe(1);

  await fileInput.setInputFiles([]);
  await expect(thumbnailData).toHaveValue('');
  await expect(thumbnailPreview).toBeHidden();
  await expect(captureButton).toBeDisabled();
  await expect(thumbSection).toBeHidden();
  await expect(video).toBeHidden();
  expect(await video.getAttribute('src')).toBeNull();
  expect(await page.evaluate(() => window.revokedThumbnailURLs.length)).toBe(2);

  await fileInput.setInputFiles({
    name: 'error-synthetic.mp4',
    mimeType: 'video/mp4',
    buffer: Buffer.from('SYNTHETIC-ERROR-VIDEO-FIXTURE'),
  });
  await video.evaluate((element) => element.dispatchEvent(new Event('loadeddata')));
  await expect(thumbnailData).toHaveValue(fixtures.thumbnailDataURL);
  await expect.poll(() => thumbnailPreview.evaluate((image) => image.naturalWidth)).toBe(640);
  await expect.poll(() => thumbnailPreview.evaluate((image) => image.complete)).toBe(true);
  await expectThumbnailBounds(page);
  await video.evaluate((element) => element.dispatchEvent(new Event('error')));
  await expect(thumbnailData).toHaveValue('');
  await expect(thumbnailPreview).toBeHidden();
  await expect(captureButton).toBeDisabled();
  expect(await page.evaluate(() => window.thumbnailEncodeCalls)).toBe(3);

  await page.evaluate(() => { window.thumbnailVideoHeight = 0; });
  await fileInput.setInputFiles({
    name: 'invalid-metadata.mp4',
    mimeType: 'video/mp4',
    buffer: Buffer.from('SYNTHETIC-INVALID-METADATA-VIDEO'),
  });
  await video.evaluate((element) => element.dispatchEvent(new Event('loadeddata')));
  await expect(thumbnailData).toHaveValue('');
  await expect(thumbnailPreview).toBeHidden();
  await expect(captureButton).toBeDisabled();
  expect(await page.evaluate(() => window.thumbnailEncodeCalls)).toBe(3);

  await page.evaluate(() => {
    window.thumbnailVideoHeight = 360;
    window.thumbnailCanvasContextAvailable = false;
  });
  await fileInput.setInputFiles({
    name: 'canvas-context-error.mp4',
    mimeType: 'video/mp4',
    buffer: Buffer.from('SYNTHETIC-CONTEXT-ERROR-VIDEO'),
  });
  await video.evaluate((element) => element.dispatchEvent(new Event('loadeddata')));
  await expect(thumbnailData).toHaveValue('');
  await expect(thumbnailPreview).toBeHidden();
  await expect(captureButton).toBeDisabled();
  expect(await page.evaluate(() => window.thumbnailEncodeCalls)).toBe(3);

  await page.evaluate(() => {
    window.thumbnailCanvasContextAvailable = true;
    window.thumbnailToDataURLThrows = true;
  });
  await fileInput.setInputFiles({
    name: 'thumbnail-encoding-error.mp4',
    mimeType: 'video/mp4',
    buffer: Buffer.from('SYNTHETIC-ENCODING-ERROR-VIDEO'),
  });
  await video.evaluate((element) => element.dispatchEvent(new Event('loadeddata')));
  await expect(thumbnailData).toHaveValue('');
  await expect(thumbnailPreview).toBeHidden();
  await expect(captureButton).toBeDisabled();
  expect(await page.evaluate(() => window.thumbnailEncodeCalls)).toBe(4);
});

test('anonymous users can fetch only registered public fixtures', async ({ page, request, contextOptions }) => {
  const fixtures = await loadFixtures(request);
  const publicWatch = await page.goto(appURL(`/watch/${fixtures.videos.public}`));
  expect(publicWatch?.status()).toBe(200);
  await expect(page.getByRole('heading', { name: 'E2E Public Video' })).toBeVisible();
  await assertConfiguredContext(page, contextOptions);
  await expectResponsiveLayout(page, true);
  const publicWatchBody = await page.locator('body').innerText();
  expect(publicWatchBody).not.toContain('E2E Video Locked');
  expect(publicWatchBody).not.toContain('E2E Channel Locked');

  for (const videoID of [
    fixtures.videos.private,
    fixtures.videos.videoLocked,
    fixtures.videos.channelLocked,
  ]) {
    const response = await getStatus(request, `/watch/${videoID}`);
    expect(response.status()).toBe(404);
  }

  const publicMedia = await getStatus(request, fixtures.media[String(fixtures.videos.public)]);
  expect(publicMedia.status()).toBe(200);
  expect((await publicMedia.body()).toString()).toContain('SYNTHETIC-E2E-MEDIA-101');
  for (const key of ['private', 'videoLocked', 'channelLocked']) {
    const mediaPath = fixtures.media[String(fixtures.videos[key])];
    const response = await getStatus(request, mediaPath);
    expect(response.status()).toBe(403);
    expect(await response.text()).not.toContain('SYNTHETIC-E2E-MEDIA');

    const uploadPath = fixtures.uploads[String(fixtures.videos[key])];
    const uploadResponse = await getStatus(request, uploadPath);
    expect(uploadResponse.status()).toBe(403);
    expect(await uploadResponse.text()).not.toContain('SYNTHETIC-E2E-UPLOAD');
  }

  const unregisteredMedia = await getStatus(request, '/media/unregistered.mp4');
  expect(unregisteredMedia.status()).toBe(404);
  expect(await unregisteredMedia.text()).not.toContain('SYNTHETIC-E2E-UNREGISTERED-MEDIA');
  const publicMediaAlias = fixtures.media[String(fixtures.videos.public)].replace('.mp4', '.MP4');
  expect((await getStatus(request, publicMediaAlias)).status()).toBe(404);
  const publicUpload = fixtures.uploads[String(fixtures.videos.public)];
  const uploadPrefixAlias = publicUpload.replace('/static/uploads/', '/static/UPLOADS/');
  expect((await getStatus(request, uploadPrefixAlias)).status()).toBe(404);
  const ownerID = fixtures.users.owner;
  expect((await getStatus(
    request,
    `/static/uploads/users/${ownerID}/999/original.mp4`,
  )).status()).toBe(404);
  expect((await getStatus(request, `/static/uploads/users/${ownerID}/`)).status()).toBe(404);

  const openAvatar = await getStatus(request, fixtures.avatars.open);
  expect(openAvatar.status()).toBe(200);
  expect((await openAvatar.body()).toString()).toContain('SYNTHETIC-E2E-OPEN-AVATAR');
  const lockedAvatar = await getStatus(request, fixtures.avatars.locked);
  expect(lockedAvatar.status()).toBe(403);
  expect(await lockedAvatar.text()).not.toContain('SYNTHETIC-E2E-LOCKED-AVATAR');
});

test('owner, other-user, and admin access follows private and lock policies', async ({
  browser,
  contextOptions,
  request,
}) => {
  const fixtures = await loadFixtures(request);
  const roles = {};
  try {
    for (const role of ['owner', 'other', 'admin']) {
      roles[role] = await createRolePage(browser, role, contextOptions);
    }

    const watchCases = [
      ['public', 'owner', 200],
      ['public', 'other', 200],
      ['public', 'admin', 200],
      ['private', 'owner', 200],
      ['private', 'other', 404],
      ['private', 'admin', 200],
      ['videoLocked', 'owner', 404],
      ['videoLocked', 'other', 404],
      ['videoLocked', 'admin', 200],
      ['channelLocked', 'owner', 404],
      ['channelLocked', 'other', 404],
      ['channelLocked', 'admin', 200],
    ];
    for (const [videoKey, role, expected] of watchCases) {
      const videoID = fixtures.videos[videoKey];
      const response = await getStatus(roles[role].page.request, `/watch/${videoID}`);
      expect(response.status(), `${role} watch ${videoKey}`).toBe(expected);
      if (videoKey === 'public') {
        const body = await response.text();
        if (role === 'admin') {
          expect(body).toContain('E2E Video Locked');
          expect(body).toContain('E2E Channel Locked');
        } else {
          expect(body).not.toContain('E2E Video Locked');
          expect(body).not.toContain('E2E Channel Locked');
        }
      }
    }

    const mediaCases = [
      ['public', 'owner', 200],
      ['public', 'other', 200],
      ['public', 'admin', 200],
      ['private', 'owner', 200],
      ['private', 'other', 403],
      ['private', 'admin', 200],
      ['videoLocked', 'owner', 200],
      ['videoLocked', 'other', 403],
      ['videoLocked', 'admin', 200],
      ['channelLocked', 'owner', 200],
      ['channelLocked', 'other', 403],
      ['channelLocked', 'admin', 200],
    ];
    for (const [videoKey, role, expected] of mediaCases) {
      const videoID = fixtures.videos[videoKey];
      const response = await getStatus(
        roles[role].page.request,
        fixtures.media[String(videoID)],
      );
      expect(response.status(), `${role} media ${videoKey}`).toBe(expected);
      if (expected !== 200) {
        expect(await response.text()).not.toContain('SYNTHETIC-E2E-MEDIA');
      }
      const uploadResponse = await getStatus(
        roles[role].page.request,
        fixtures.uploads[String(videoID)],
      );
      expect(uploadResponse.status(), `${role} upload ${videoKey}`).toBe(expected);
      if (expected !== 200) {
        expect(await uploadResponse.text()).not.toContain('SYNTHETIC-E2E-UPLOAD');
      }
    }

    for (const role of ['owner', 'admin']) {
      const ownerID = fixtures.users.owner;
      for (const path of [
        '/media/unregistered.mp4',
        `/static/uploads/users/${ownerID}/999/original.mp4`,
      ]) {
        expect((await getStatus(roles[role].page.request, path)).status()).toBe(404);
      }
    }
    expect((await getStatus(roles.other.page.request, fixtures.avatars.locked)).status()).toBe(403);
    for (const role of ['owner', 'admin']) {
      const response = await getStatus(roles[role].page.request, fixtures.avatars.locked);
      expect(response.status()).toBe(200);
      expect((await response.body()).toString()).toContain('SYNTHETIC-E2E-LOCKED-AVATAR');
    }

    const suffix = randomUUID();
    const commentCases = [
      ['public', 'other', 303],
      ['private', 'owner', 303],
      ['private', 'other', 404],
      ['private', 'admin', 303],
      ['videoLocked', 'owner', 404],
      ['videoLocked', 'other', 404],
      ['videoLocked', 'admin', 303],
      ['channelLocked', 'owner', 404],
      ['channelLocked', 'other', 404],
      ['channelLocked', 'admin', 303],
      ['commentsDisabled', 'owner', 403],
      ['commentsDisabled', 'other', 403],
      ['commentsDisabled', 'admin', 403],
    ];
    const comments = [];
    for (const [videoKey, role, expected] of commentCases) {
      const content = `E2E-${suffix}-${videoKey}-${role}`;
      const response = await postComment(
        roles[role].page.request,
        fixtures.videos[videoKey],
        content,
      );
      expect(response.status(), `${role} comment ${videoKey}`).toBe(expected);
      comments.push({ videoKey, content, accepted: expected === 303 });
    }

    for (const videoKey of ['private', 'videoLocked', 'channelLocked']) {
      await roles.admin.page.goto(appURL(`/watch/${fixtures.videos[videoKey]}`));
      await expectResponsiveLayout(roles.admin.page, true);
      for (const comment of comments.filter((item) => item.videoKey === videoKey)) {
        const locator = roles.admin.page.getByText(comment.content, { exact: true });
        if (comment.accepted) {
          await expect(locator).toBeVisible();
        } else {
          await expect(locator).toHaveCount(0);
        }
      }
    }
  } finally {
    await Promise.all(Object.values(roles).map(({ context }) => context.close()));
  }
});

test('private video comment UI is hidden from other users and crafted posts are denied', async ({
  browser,
  contextOptions,
  page,
  request,
}) => {
  const fixtures = await loadFixtures(request);
  await login(page, 'owner');
  const ownerWatch = await page.goto(appURL(`/watch/${fixtures.videos.private}`));
  expect(ownerWatch?.status()).toBe(200);
  await expect(page.getByRole('heading', { name: 'E2E Private Video' })).toBeVisible();
  await assertConfiguredContext(page, contextOptions);
  await waitForPrerollDismissal(page);
  await expectResponsiveLayout(page, true);
  await expect(page.getByPlaceholder('Add a comment...')).toBeVisible();

  const visibleComment = `E2E-private-owner-${randomUUID()}`;
  await page.getByPlaceholder('Add a comment...').fill(visibleComment);
  await page.getByRole('button', { name: 'Comment' }).click();
  await expect(page.getByText(visibleComment, { exact: true })).toBeVisible();

  const otherContext = await browser.newContext({ ...contextOptions, serviceWorkers: 'block' });
  try {
    await blockNonLocalRequests(otherContext);
    const otherPage = await otherContext.newPage();
    await login(otherPage, 'other');
    const privateWatch = await otherPage.goto(appURL(`/watch/${fixtures.videos.private}`));
    expect(privateWatch?.status()).toBe(404);

    const craftedComment = `E2E-private-other-${randomUUID()}`;
    const response = await postComment(
      otherPage.request,
      fixtures.videos.private,
      craftedComment,
    );
    expect(response.status()).toBe(404);

    await page.reload();
    await expect(page.getByText(visibleComment, { exact: true })).toBeVisible();
    await expect(page.getByText(craftedComment, { exact: true })).toHaveCount(0);
  } finally {
    await otherContext.close();
  }
});

test("channel owner can remove another user's public comment from the watch page", async ({
  browser,
  contextOptions,
  page,
}) => {
  const other = await createRolePage(browser, 'other', contextOptions);
  try {
    const suffix = randomUUID();
    const comment = `E2E-public-comment-${suffix}`;
    await other.page.goto(appURL('/watch/101'));
    await expect(other.page.getByRole('heading', { name: 'E2E Public Video' })).toBeVisible();
    await waitForPrerollDismissal(other.page);
    await expectResponsiveLayout(other.page, true);
    await other.page.getByPlaceholder('Add a comment...').fill(comment);
    await other.page.getByRole('button', { name: 'Comment' }).click();
    await expect(other.page.getByText(comment, { exact: true })).toBeVisible();

    const otherComment = other.page.locator('.comment-item').filter({ hasText: comment });
    const deleteAction = await otherComment.locator('form.delete-comment-form').getAttribute('action');
    expect(deleteAction).toMatch(/^\/delete-comment\/\d+$/);

    await login(page, 'owner');
    const ownerWatch = await page.goto(appURL('/watch/101'));
    expect(ownerWatch?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'E2E Public Video' })).toBeVisible();
    await assertConfiguredContext(page, contextOptions);
    await waitForPrerollDismissal(page);
    await expectResponsiveLayout(page, true);
    const ownerComment = page.locator('.comment-item').filter({ hasText: comment });
    await expect(ownerComment).toBeVisible();
    const ownerDeleteForm = ownerComment.locator('form.delete-comment-form');
    await expect(ownerDeleteForm).toHaveAttribute('action', deleteAction);
    page.once('dialog', (dialog) => dialog.accept());
    await ownerDeleteForm.getByRole('button', { name: 'Delete' }).click();
    await expect(page).toHaveURL(/\/watch\/101$/);
    await expect(page.getByText(comment, { exact: true })).toHaveCount(0);
  } finally {
    await other.context.close();
  }
});

test('registration creates a normal account without administrative edit access', async ({
  page,
  contextOptions,
}) => {
  const email = `registered-${randomUUID()}@e2e.test`;
  await page.goto(appURL('/register'));
  await assertConfiguredContext(page, contextOptions);
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill('Synthetic-E2E-Register-42');
  await page.getByLabel('Location').fill('Sofia');
  await page.getByRole('button', { name: 'Register' }).click();
  await expect(page).toHaveURL(appURL('/'));
  await expect(page.locator('.nav-text')).toContainText(email);
  await page.getByRole('link', { name: 'Upload your videos' }).click();
  await expect(page.getByRole('heading', { name: 'Upload a Video' })).toBeVisible();
  await expectResponsiveLayout(page);

  const editResponse = await getStatus(page.request, '/edit-video/103');
  expect(editResponse.status()).toBe(404);
});
