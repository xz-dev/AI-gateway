// 在JS已禁用、加载TestModelsTableSSR输出HTML的Playwright page上运行。
// 夹具用oauth/限定名走真实准入，并在00行验证大整数文本精度。
async (page) => {
  const check = (ok, message) => { if (!ok) throw new Error(message); };
  await page.locator('#tbl:not([hidden])').waitFor();
  const result = await page.evaluate(() => ({
    headers: [...document.querySelectorAll('th')].map(x => x.textContent),
    rows: [...document.querySelectorAll('tbody tr')].map(r => [...r.cells].map(c => c.textContent)),
    meta: document.querySelector('#meta').textContent,
    activeHTML: document.querySelectorAll('#tbl img,#tbl script,#tbl svg').length,
    executed: window.__executed === 1,
    scripts: document.scripts.length,
    requests: performance.getEntriesByType('resource').filter(x => x.name.includes('/v1/models')).map(x => new URL(x.name).pathname + new URL(x.name).search),
    border: getComputedStyle(document.querySelector('td')).borderTopWidth
  }));
  check(result.headers.length === 11 && result.rows.length === 11, 'counts');
  check(JSON.stringify(result.headers.slice(6,9)) === JSON.stringify(['max_tokens(abandon)','max_completion_tokens','max_output_tokens']), 'output columns');
  check(JSON.stringify(result.rows.map(r => r[0])) === JSON.stringify(['00','01','02','03','04','05','06-vision','07-image','08-audio','09-video','10-hostile'].map(x => 'oauth/' + x)), 'sort/filter');
  check(JSON.stringify(result.rows.slice(0,6).map(r => r[2])) === JSON.stringify(['未知','null','[]','false','0','""']), 'states');
  check(JSON.stringify(result.rows[0].slice(6,9)) === JSON.stringify(['128000','未知','未知']), 'legacy-only output');
  check(JSON.stringify(result.rows[1].slice(6,9)) === JSON.stringify(['null','0','未知']) && result.rows[1][4] === 'null', 'output null/zero/missing');
  check(JSON.stringify(result.rows[6].slice(6,9)) === JSON.stringify(['4096','16384','8192']), 'independent output values');
  check(result.rows[6][2] === '["text","image"]' && result.rows[6][3] === '["text"]' && result.rows[7][3] === '["image"]', 'directional modalities');
  check(result.rows[10][1] === '<img src=x onerror="window.__executed=1"><script>window.__executed=1</script>' && !result.activeHTML && !result.executed, 'unsafe rendering');
  check(result.requests.length === 0 && result.scripts === 0, 'pure SSR contract');
  check(result.rows[0][4] === '9007199254740993', 'numeric text precision');
  check(result.border === '1px', 'border');
  const origin = page.url().split('/').slice(0,3).join('/');
  for (const path of ['/table-error', '/table-network-error']) {
    const response = await page.goto(origin + path);
    check(response.status() === 502, 'HTTP error status');
    await page.getByText('HTTP 502', {exact:true}).waitFor();
    check((await page.locator('#err').textContent()).includes('native_manifest_failed'), 'error text');
    check(await page.locator('#tbl').count() === 0, 'error must not show a success table');
  }
  return {passed:true, httpError:true, networkError:true, ...result};
}
