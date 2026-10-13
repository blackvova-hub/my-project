// Two real browser peers talk through the deployed self-hosted LiveKit SFU.
// Fake microphone hardware generates test audio; APIs, signaling and media are real.
import { chromium, expect } from "@playwright/test";
import { readFileSync, mkdirSync, writeFileSync } from "node:fs";
const base = process.env.VOICE_E2E_BASE ?? "https://192.168.0.130";
const output = process.env.VOICE_E2E_OUTPUT ?? "/output";
const access = JSON.parse(readFileSync(process.env.VOICE_E2E_ACCESS_FILE, "utf8"));
const secondEmail = process.env.VOICE_E2E_SECOND_EMAIL;
if (!secondEmail) throw new Error("An isolated second test account is required");
mkdirSync(output, { recursive: true });
const browser = await chromium.launch({ channel: "chromium", headless: true, ignoreDefaultArgs: ["--mute-audio"], args: ["--use-fake-device-for-media-stream", "--autoplay-policy=no-user-gesture-required", ...(process.env.VOICE_E2E_AUDIO_FILE ? [`--use-file-for-fake-audio-capture=${process.env.VOICE_E2E_AUDIO_FILE}`] : [])] });
const contexts = [], checks = [], errors = [];
const check = name => { checks.push(name); console.log("PASS", name); };
async function peer(email, permission = true) {
  const context = await browser.newContext({ viewport: { width: 1536, height: 1024 }, ignoreHTTPSErrors: true, reducedMotion: "reduce", permissions: permission ? ["microphone"] : [] });
  contexts.push(context); await context.addInitScript(() => { localStorage.setItem("cookie_consent", "essential"); localStorage.setItem("mobile_notice_shown_v1", "1"); localStorage.setItem("site-theme", "dark"); });
  const page = await context.newPage(); page.on("pageerror", e => errors.push(e.message));
  await page.goto(`${base}/login`);
  await page.getByLabel("Email", { exact: true }).fill(email); await page.getByLabel("Пароль", { exact: true }).fill(access.password);
  await page.getByRole("button", { name: "Войти", exact: true }).click(); await page.waitForURL(u => !u.pathname.includes("login"));
  await page.goto(`${base}/community/voice`); await expect(page.locator('[data-pair="BTC-USDT"]')).toBeVisible({ timeout: 120000 });
  return { page, context };
}
const rooms = async context => (await (await context.request.get(`${base}/api/community/voice/rooms`)).json()).rooms;
const join = async page => { await page.getByRole("button", { name: "Войти в комнату", exact: true }).click(); await expect(page.locator('[data-voice-state="connected"]')).toBeVisible({ timeout: 30000 }); };
let sourceCount, totalPairs, energy;
try {
  const anonymous = await browser.newContext({ ignoreHTTPSErrors: true }); contexts.push(anonymous);
  expect((await anonymous.request.get(`${base}/api/community/voice/pairs`)).status()).toBe(401); check("anonymous token/catalogue access rejected");
  const a = await peer(access.email ?? "admin@local.dev"), b = await peer(secondEmail);
  const catalogue = await (await a.context.request.get(`${base}/api/community/voice/pairs`)).json();
  expect(catalogue.items.length).toBeGreaterThan(1000); expect(new Set(catalogue.items.map(p => p.id)).size).toBe(catalogue.items.length);
  sourceCount = catalogue.sources.filter(s => s.available).length; totalPairs = catalogue.items.length;
  check("real exchange catalogue has unique pair IDs across spot and derivatives");
  expect((await rooms(a.context))["BTC-USDT"] ?? 0).toBe(0);
  const grantResponse = await a.context.request.post(`${base}/api/community/voice/join`, { data: { pair: "BTC-USDT" } }); expect(grantResponse.status()).toBe(200);
  expect((await rooms(a.context))["BTC-USDT"] ?? 0).toBe(0); check("browsing and token issuance do not provision rooms");
  expect((await a.context.request.post(`${base}/api/community/voice/join`, { data: { pair: "INVALID-USDT" } })).status()).toBe(404);
  expect((await a.context.request.post(`${base}/api/community/voice/join`, { data: { pair: "BTC-USDT" }, headers: { Origin: "https://foreign.local" } })).status()).toBe(403);
  expect((await a.context.request.post(`${base}/livekit/twirp/livekit.RoomService/ListRooms`, { data: {} })).status()).toBe(404); check("invalid room, foreign origin and public room-admin requests rejected");
  await a.page.getByLabel("Найти пару", { exact: true }).fill("BTC / USDT"); await expect(a.page.locator('[data-pair="BTC-USDT"]')).toHaveCount(1); const matches = await a.page.locator('[data-pair]').evaluateAll(nodes => nodes.map(n=>n.dataset.pair)); expect(new Set(matches).size).toBe(matches.length); await a.page.getByRole("button", { name: "Очистить поиск" }).click(); check("pair search handles separators without duplicate rooms");
  for (const theme of ["dark", "light", "green"]) {
    await a.page.getByRole("button", { name: { dark: "Тёмная тема", light: "Светлая тема", green: "Фирменная зелёная тема" }[theme], exact: true }).click();
    await expect(a.page.locator("html")).toHaveAttribute("data-theme", theme);
    await a.page.locator('[data-pair="ETH-USDT"]').hover();
    await a.page.getByLabel("Найти пару", { exact: true }).focus();
    await a.page.screenshot({ path: `${output}/${theme}.png` });
  }
  await a.page.getByRole("button", { name: "Тёмная тема", exact: true }).click();
  await join(a.page); await expect(a.page.locator("[data-voice-member]")).toHaveCount(1);
  await expect(a.page.getByRole("button", { name: "Включить микрофон", exact: true })).toHaveAttribute("aria-pressed", "false");
  expect(await a.page.locator("[data-voice-audio-host] audio").count()).toBe(0); check("first participant creates a room and enters with microphone off");
  await join(b.page); await expect(a.page.locator("[data-voice-member]")).toHaveCount(2); await expect(b.page.locator("[data-voice-member]")).toHaveCount(2);
  await expect.poll(async () => (await rooms(a.context))["BTC-USDT"], { timeout: 15000 }).toBe(2); check("two authenticated peers connect to the same SFU room");
  const capture = await a.page.evaluate(async () => { try { const media = await navigator.mediaDevices.getUserMedia({ audio: true }); const live = media.getAudioTracks().some(t=>t.readyState === "live"); media.getTracks().forEach(t=>t.stop()); return { live }; } catch(e) { return { name: e.name, message: e.message }; } });
  console.log("CAPTURE_CHECK", JSON.stringify(capture)); expect(capture.live).toBe(true);
  await a.page.getByRole("button", { name: "Включить микрофон", exact: true }).click();
  await expect(a.page.getByRole("button", { name: "Выключить микрофон", exact: true })).toHaveAttribute("aria-pressed", "true");
  await expect(b.page.locator("[data-voice-audio-host] audio")).toHaveCount(1, { timeout: 20000 });
  energy = await b.page.evaluate(async () => {
    const audio = document.querySelector("[data-voice-audio-host] audio");
    if (!audio?.srcObject?.getAudioTracks().length) throw new Error("No remote MediaStream audio track");
    const ctx = new AudioContext(); await ctx.resume(); const source = ctx.createMediaStreamSource(audio.srcObject); const analyser = ctx.createAnalyser(); analyser.fftSize = 2048; source.connect(analyser);
    const samples = new Float32Array(analyser.fftSize); let peak = 0;
    for (let i=0;i<80;i++) { await new Promise(r=>setTimeout(r,50)); analyser.getFloatTimeDomainData(samples); let sum=0; for (const v of samples) sum+=v*v; peak=Math.max(peak,Math.sqrt(sum/samples.length)); }
    source.disconnect(); await ctx.close(); return peak;
  });
  expect(energy).toBeGreaterThan(0.001); check("remote decoded audio has measurable signal after real WebRTC transport");
  await expect.poll(() => a.page.locator('[data-speaking="true"]').count(), { timeout: 12000 }).toBeGreaterThan(0); check("speaking indicator follows live audio activity");
  await b.page.getByLabel("Громкость участников").focus(); await b.page.getByLabel("Громкость участников").press("Home");
  for(let i=0;i<25;i++) await b.page.getByLabel("Громкость участников").press("ArrowRight");
  await expect.poll(() => b.page.locator("[data-voice-audio-host] audio").evaluateAll(nodes => nodes[0]?.volume)).toBe(0.25);
  await b.page.getByRole("button", { name: "Выключить звук", exact: true }).click(); await expect.poll(() => b.page.locator("[data-voice-audio-host] audio").evaluateAll(nodes=>nodes[0]?.volume)).toBe(0);
  await b.page.getByRole("button", { name: "Включить звук", exact: true }).click(); check("volume and deafen control remote playback");
  await a.page.screenshot({ path: `${output}/connected.png` });
  await b.page.locator('[data-pair="ETH-USDT"]').click(); await b.page.getByRole("button", { name: "Перейти в эту комнату", exact: true }).click();
  await expect(b.page.locator('[data-voice-state="connected"]')).toBeVisible(); await expect(b.page.locator("[data-voice-member]")).toHaveCount(1); await expect(a.page.locator("[data-voice-member]")).toHaveCount(1); check("switching rooms disconnects the old room before joining another");
  await b.page.getByRole("button", { name: "Выйти", exact: true }).click(); await a.page.getByRole("button", { name: "Выйти", exact: true }).click();
  await expect.poll(async () => Object.keys(await rooms(a.context)).length, { timeout: 15000 }).toBe(0); check("last participant leaves and empty rooms disappear from active list");
  const denied = await peer(secondEmail, false); await join(denied.page); await denied.page.getByRole("button", { name: "Включить микрофон", exact: true }).click();
  await expect(denied.page.getByRole("alert")).toContainText("Нет доступа к микрофону", { timeout: 15000 }); await expect(denied.page.locator('[data-voice-state="connected"]')).toBeVisible(); check("denied microphone keeps listening session usable and explains recovery");
  await denied.page.goto(`${base}/community`); await expect.poll(async () => Object.keys(await rooms(a.context)).length, { timeout: 15000 }).toBe(0); check("leaving the voice page releases the connection and media devices");
  await a.page.setViewportSize({ width: 390, height: 844 }); await a.page.getByRole("button", { name: "С участниками", exact: true }).click();
  await expect(a.page.getByText("Сейчас нет занятых комнат.", { exact: false })).toBeVisible(); await a.page.screenshot({ path: `${output}/mobile.png`, fullPage: true });
  expect(await a.page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true); check("mobile layout has no horizontal overflow and handles no occupied rooms");
  expect(errors).toEqual([]); check("no browser runtime errors");
  writeFileSync(`${output}/verification.json`, JSON.stringify({ checks, totalPairs, availableSources: sourceCount, remoteAudioPeakRMS: energy, browserErrors: errors, base, testedAt: new Date().toISOString() }, null, 2));
} catch(e) { console.error(e); for (let i=0;i<contexts.length;i++) { const page=contexts[i].pages()[0]; if(page) await page.screenshot({ path: `${output}/failure-${i}.png`, fullPage: true }).catch(()=>{}); } throw e; }
finally { for (const context of contexts) await context.close(); await browser.close(); }
