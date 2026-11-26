// Último uso/status por alias (#55). Roda com `pnpm run test`.
// @ts-expect-error sem @types/node, como no vite.config.js
import { test } from "node:test";
// @ts-expect-error sem @types/node
import assert from "node:assert/strict";
import { usageLabel, usageRefused } from "../src/lib/usage.ts";

const NOW = Date.parse("2026-11-26T12:00:00Z");

test("sem uso registrado não mostra nada", () => {
  assert.equal(usageLabel({}, NOW), "");
  assert.equal(usageLabel({ last_used_at: "2026-11-26T11:55:00Z" }, NOW), "");
});

test("mostra quando e o status", () => {
  assert.equal(usageLabel({ last_used_at: "2026-11-26T11:55:00Z", last_status: 200 }, NOW), "usado há 5 minutos · HTTP 200");
  assert.equal(usageLabel({ last_used_at: "2026-11-26T09:00:00Z", last_status: 429 }, NOW), "usado há 3 horas · HTTP 429");
  assert.equal(usageLabel({ last_used_at: "lixo", last_status: 500 }, NOW), "HTTP 500");
});

test("401 e 403 contam como chave recusada", () => {
  assert.equal(usageRefused({ last_status: 401 }), true);
  assert.equal(usageRefused({ last_status: 403 }), true);
  assert.equal(usageRefused({ last_status: 200 }), false);
  assert.equal(usageRefused({}), false);
});
