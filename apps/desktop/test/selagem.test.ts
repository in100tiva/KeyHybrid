// Texto de selagem por plataforma (#39). Roda com `pnpm run test`.
// @ts-expect-error sem @types/node, como no envImport.test.ts
import { test } from "node:test";
// @ts-expect-error sem @types/node
import assert from "node:assert/strict";
import { textoSelagem } from "../src/lib/selagem.ts";

const UA_WINDOWS =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0";
const UA_LINUX = "Mozilla/5.0 (X11; Ubuntu; Linux x86_64) AppleWebKit/605.1.15 (KHTML, like Gecko)";

test("Windows mantém o DPAPI", () => {
  const t = textoSelagem(UA_WINDOWS);
  assert.equal(t.selo, "DPAPI");
  assert.match(t.pinSalvo, /DPAPI/);
});

test("Linux não fala em DPAPI e avisa que não há selagem", () => {
  const t = textoSelagem(UA_LINUX);
  for (const texto of Object.values(t)) assert.doesNotMatch(texto, /DPAPI/);
  assert.match(t.rodape, /sem selagem/);
  assert.match(t.rodape, /LUKS/);
});
