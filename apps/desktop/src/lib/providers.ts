// Provedores conhecidos pelo cofre: valem para o cadastro manual e para a importação de .env.

// Serviços (não-IA): URL base predefinida e variável sugerida para o `kh run`
export const SERVICES: Record<string, { baseUrl: string; envVar?: string }> = {
  // OpenAI é serviço: no /v1 do gateway, provedor sem rota própria cai na OpenRouter (#17)
  openai: { baseUrl: "https://api.openai.com", envVar: "OPENAI_API_KEY" },
  vercel: { baseUrl: "https://api.vercel.com", envVar: "VERCEL_TOKEN" },
  supabase: { baseUrl: "https://api.supabase.com", envVar: "SUPABASE_ACCESS_TOKEN" },
  github: { baseUrl: "https://api.github.com", envVar: "GH_TOKEN" },
  outro: { baseUrl: "" },
};

// shortcut: só prefixos conhecidos; Vercel não tem prefixo fixo, fica manual
const PROVIDER_PREFIXES: [string, string][] = [
  ["sk-or-", "openrouter"],
  ["sk-ant-", "anthropic"],
  ["sk-proj-", "openai"],
  ["sk-svcacct-", "openai"],
  ["gsk_", "groq"],
  ["AIza", "gemini"],
  ["sbp_", "supabase"],
  ["ghp_", "github"],
  ["github_pat_", "github"],
  ["gho_", "github"],
  ["ghu_", "github"],
  ["ghs_", "github"],
  ["ghr_", "github"],
];

export function detectProvider(secret: string): string | null {
  const s = secret.trim();
  return PROVIDER_PREFIXES.find(([prefix]) => s.startsWith(prefix))?.[1] ?? null;
}
