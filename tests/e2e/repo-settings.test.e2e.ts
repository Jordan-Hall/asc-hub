// @watch start
// templates/webhook/{new,kind_fields,send_groups}.tmpl
// templates/repo/settings/**
// web_src/css/{form,repo}.css
// web_src/css/modules/grid.css
// web_src/js/features/comp/WebHookEditor.js
// @watch end

import {expect} from '@playwright/test';
import {test} from './utils_e2e.ts';
import {screenshot} from './shared/screenshots.ts';
import {validate_form} from './shared/forms.ts';

test.use({user: 'user2'});

test('repo webhook settings', async ({page}) => {
  const response = await page.goto('/user2/repo1/settings/hooks/forgejo/new');
  expect(response?.status()).toBe(200);

  // the events it sends: pushes and landed changes are ticked
  await expect(page.locator('input[name="asc_events"][value="push"]')).toBeChecked();
  await expect(page.locator('input[name="asc_events"][value="change.landed"]')).toBeChecked();
  await expect(page.locator('input[name="asc_events"][value="change.opened"]')).not.toBeChecked();
  await expect(page.locator('input[name="active"]')).toBeChecked();

  await validate_form({page}, 'fieldset');
  await screenshot(page);

  // a GET has no body, so it has no content type
  await page.locator('#http_method').selectOption('GET');
  await expect(page.locator('#content_type')).toBeHidden();
  await page.locator('#http_method').selectOption('POST');
  await expect(page.locator('#content_type')).toBeVisible();

  // the other kinds
  await page.locator('details summary').filter({hasText: 'Other kinds'}).click();
  await expect(page.locator('.wh-kinds a[aria-current="page"]')).toHaveAttribute('href', '/user2/repo1/settings/hooks/forgejo/new');
  await screenshot(page);
});

test.describe('repo branch protection settings', () => {
  test.afterEach(async ({page}) => {
    // delete the rule for the next test
    await page.goto('/user2/repo1/settings/branches/');
    await page.waitForLoadState('domcontentloaded');
    const deleteButton = page.locator('.delete-button').first();
    test.skip(await deleteButton.isHidden(), 'Nothing to delete at this time');
    await deleteButton.click();
    await page.locator('#delete-protected-branch .actions .ok').click();
    // Here page.waitForLoadState('domcontentloaded') does not work reliably.
    // Instead, wait for the delete button to disappear.
    await expect(deleteButton).toHaveCount(0);
  });

  test('form', async ({page}) => {
    const response = await page.goto('/user2/repo1/settings/branches/edit');
    expect(response?.status()).toBe(200);

    await validate_form({page}, 'fieldset');

    // verify header is new
    await expect(page.locator('h4')).toContainText('new');
    await page.locator('input[name="rule_name"]').fill('testrule');
    await screenshot(page);
    await page.locator('button:text("Save rule")').click();
    // verify header is in edit mode
    await page.waitForLoadState('domcontentloaded');
    await screenshot(page);

    // find the edit button and click it
    const editButton = page.locator('a[href="/user2/repo1/settings/branches/edit?rule_name=testrule"]');
    await editButton.click();

    await page.waitForLoadState();
    await expect(page.locator('.repo-setting-content .header')).toContainText('Protection rules for branch', {ignoreCase: true, useInnerText: true});
    await screenshot(page);
  });
});
