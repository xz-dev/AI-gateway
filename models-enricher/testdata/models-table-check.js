// 在已用 models-table.json fixture 加载当前真实 HTML 的 Playwright page 上运行。
async (page) => {
  const check = (ok, message) => { if (!ok) throw new Error(message); };
  await page.locator('#tbl:not([hidden])').waitFor();
  const result = await page.evaluate(() => ({
    headers: [...document.querySelectorAll('th')].map(x => x.textContent),
    rows: [...document.querySelectorAll('tbody tr')].map(r => [...r.cells].map(c => c.textContent)),
    meta: document.querySelector('#meta').textContent,
    activeHTML: document.querySelectorAll('#tbl img,#tbl script,#tbl svg').length,
    executed: window.__executed === 1,
    requests: performance.getEntriesByType('resource').filter(x => x.name.includes('/v1/models')).map(x => new URL(x.name).pathname + new URL(x.name).search),
    border: getComputedStyle(document.querySelector('td')).borderTopWidth
  }));
  check(result.headers.length === 9 && result.rows.length === 11, 'counts');
  check(JSON.stringify(result.rows.map(r => r[0])) === JSON.stringify(['00','01','02','03','04','05','06-vision','07-image','08-audio','09-video','10-hostile']), 'sort/filter');
  check(JSON.stringify(result.rows.slice(0,6).map(r => r[2])) === JSON.stringify(['未知','null','[]','false','0','""']), 'states');
  check(result.rows[0][6] === '未知' && result.rows[1][4] === 'null', 'canonical limits/null');
  check(result.rows[6][2] === '["text","image"]' && result.rows[6][3] === '["text"]' && result.rows[7][3] === '["image"]', 'directional modalities');
  check(result.rows[10][1] === '<img src=x onerror="window.__executed=1"><script>window.__executed=1</script>' && !result.activeHTML && !result.executed, 'unsafe rendering');
  check(result.requests.length === 1 && result.requests[0] === '/v1/models?client_version=v0.65.0', 'request contract');
  check(result.border === '1px', 'border');
  const origin = page.url().split('/').slice(0,3).join('/');
  const pattern = origin + '/v1/models?*';
  await page.route(pattern, route => route.fulfill({status:503, body:'fixture unavailable'}));
  await page.reload();
  await page.getByText('HTTP 503', {exact:true}).waitFor();
  check(await page.locator('#err').textContent() === 'fixture unavailable', 'HTTP error');
  await page.unroute(pattern);
  await page.route(pattern, route => route.abort('failed'));
  await page.reload();
  await page.getByText('fetch failed', {exact:true}).waitFor();
  check(!!(await page.locator('#err').textContent()), 'network error');
  await page.unroute(pattern);
  return {passed:true, httpError:true, networkError:true, ...result};
}
