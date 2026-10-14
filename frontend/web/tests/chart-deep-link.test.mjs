import test from 'node:test';
import assert from 'node:assert/strict';
import { chartInstrumentFromSearch } from '../src/pages/public/charts/deepLink.ts';
import { loginDestination } from '../src/shared/auth/loginDestination.ts';

test('news links open the requested real exchange and market', () => {
  assert.deepEqual(chartInstrumentFromSearch('?symbol=ethusdt&exchange=bybit&market=spot'), {symbol:'ETHUSDT',exchange:'bybit',market:'spot'});
  assert.deepEqual(chartInstrumentFromSearch('?symbol=BTCUSDT'), {symbol:'BTCUSDT',exchange:'bybit',market:'linear'});
});
test('malformed or unsupported instruments cannot overwrite a saved workspace', () => {
  for (const query of ['', '?symbol=../../BTCUSDT', '?symbol=BTCUSDT&exchange=unknown', '?symbol=BTCUSDT&market=inverse', '?symbol=DXY']) {
    assert.equal(chartInstrumentFromSearch(query), null);
  }
});
test('login keeps instrument queries while refusing external redirect targets', () => {
  assert.equal(loginDestination('/charts?symbol=ETHUSDT&exchange=bybit&market=spot'), '/charts?symbol=ETHUSDT&exchange=bybit&market=spot');
  assert.equal(loginDestination('/news'), '/news');
  for (const target of ['https://evil.example', '//evil.example', '/\\evil.example', null, '/news\n']) assert.equal(loginDestination(target), '/account');
});
