// Importação de .env para o cofre (#18): lê o arquivo, reconhece as chaves e grava pelo
// mesmo caminho do cadastro manual. Puro (sem Svelte nem Tauri) para rodar em `node --test`.
import { SERVICES, detectProvider } from "./providers.ts";

// Nome de variável conhecido → provedor do cofre (Map: nome do .env não pode cair no Object.prototype)
const ENV_NAMES = new Map<string, string>([
  ["OPENROUTER_API_KEY", "openrouter"],
  ["ANTHROPIC_API_KEY", "anthropic"],
  ["GEMINI_API_KEY", "gemini"],
  ["GOOGLE_API_KEY", "gemini"],
  ["GOOGLE_GENERATIVE_AI_API_KEY", "gemini"],
  ["OPENAI_API_KEY", "openai"],
  ["GITHUB_TOKEN", "github"],
  ["GH_TOKEN", "github"],
  ["SUPABASE_ACCESS_TOKEN", "supabase"],
  ["VERCEL_TOKEN", "vercel"],
]);

const LINE = /^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/;

export interface ImportRow {
  names: string[]; // variáveis do .env com esta chave (a mesma chave pode vir em duas)
  provider: string;
  kind: "ai" | "service";
  baseUrl: string | null;
  alias: string; // livre no cofre: a gravação é upsert por alias e não pode cair em cima de outra chave
  masked: string; // só o final da chave, como o gateway mostra
}

export interface ImportPlan {
  rows: ImportRow[];
  leftOut: string[]; // variáveis que não são chave conhecida: continuam só no .env
}

// Lê o .env como o dotenv: `export`, aspas, comentário e CRLF; variável repetida fica com o último valor
export function parseEnv(text: string): Map<string, string> {
  const vars = new Map<string, string>();
  for (const line of text.replace(/^﻿/, "").split(/\r?\n/)) {
    const m = LINE.exec(line);
    if (!m) continue;
    const raw = m[2].trim();
    const quoted = /^(["'`])(.*?)\1/.exec(raw);
    const value = (quoted ? quoted[2] : raw.replace(/#.*/, "")).trim();
    if (value) vars.set(m[1], value);
  }
  return vars;
}

// O prefixo diz quem emitiu a chave e vence o nome: a chave só vai para quem a emitiu.
// "AIza" é de qualquer API do Google (Maps, Firebase...): sozinho, não basta para virar Gemini.
function providerFor(name: string, value: string): string | null {
  const byName = ENV_NAMES.get(name) ?? null;
  const byValue = detectProvider(value);
  if (byValue === "gemini" && !byName) return null;
  return byValue ?? byName;
}

export function planImport(text: string, existingAliases: string[]): ImportPlan {
  const taken = new Set(existingAliases.map((a) => a.toLowerCase()));
  const bySecret = new Map<string, ImportRow>();
  const leftOut: string[] = [];
  for (const [name, value] of parseEnv(text)) {
    const same = bySecret.get(value);
    if (same) {
      same.names.push(name); // mesma chave em outra variável: uma entrada no cofre, as duas no kh run
      continue;
    }
    const provider = providerFor(name, value);
    if (!provider) {
      leftOut.push(name);
      continue;
    }
    let alias = provider;
    for (let n = 2; taken.has(alias); n++) alias = `${provider}-${n}`;
    taken.add(alias);
    const service = SERVICES[provider];
    bySecret.set(value, {
      names: [name],
      provider,
      kind: service ? "service" : "ai",
      baseUrl: service?.baseUrl ?? null,
      alias,
      masked: value.length > 12 ? `••••••••${value.slice(-4)}` : "••••••••",
    });
  }
  return { rows: [...bySecret.values()], leftOut };
}

// Grava em sequência e para no primeiro erro. A chave sai do texto colado direto para o
// `save` (o mesmo caminho do cadastro manual): não fica na prévia, em log nem em armazenamento.
export async function importRows(
  rows: ImportRow[],
  text: string,
  save: (row: ImportRow, secret: string) => Promise<unknown>,
): Promise<{ done: ImportRow[]; error: string }> {
  const vars = parseEnv(text);
  const done: ImportRow[] = [];
  for (const row of rows) {
    try {
      await save(row, vars.get(row.names[0]) ?? "");
    } catch (err) {
      return { done, error: String(err) };
    }
    done.push(row);
  }
  return { done, error: "" };
}

// Roda o projeto sem o .env: cada variável volta do cofre só para o processo filho
export function khRunCommand(rows: ImportRow[]): string {
  const envs = rows.flatMap((r) => r.names.map((n) => `-e ${n}=${r.alias}`));
  return `kh run ${envs.join(" ")} -- <comando>`;
}
