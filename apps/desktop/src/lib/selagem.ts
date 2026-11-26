// Textos da proteção em repouso por plataforma (#39). Só o Windows sela a chave mestra (DPAPI);
// fora dele ela é um arquivo 0600 sem selagem (selagem pelo Secret Service planejada na #40).

export interface TextoSelagem {
  pinSalvo: string;
  definirPin: string;
  rodape: string;
  selo: string;
  cofre: string;
}

const WINDOWS: TextoSelagem = {
  pinSalvo: "PIN de 6 dígitos salvo no Windows DPAPI.",
  definirPin: "Defina um código de 6 dígitos para proteger suas chaves no DPAPI",
  rodape: "Cofre protegido via Windows DPAPI",
  selo: "DPAPI",
  cofre: "Cofre Windows DPAPI",
};

const SEM_SELAGEM: TextoSelagem = {
  pinSalvo: "PIN de 6 dígitos salvo (hash Argon2id, arquivo 0600).",
  definirPin: "Defina um código de 6 dígitos para bloquear o acesso às suas chaves",
  rodape: "Chave mestra em arquivo 0600, sem selagem do sistema. Faça backup .khvault e cifre o disco (LUKS).",
  selo: "0600",
  cofre: "Cofre local (chave 0600, sem selagem)",
};

/** Recebe o navigator.userAgent; o webview do Tauri no Windows (WebView2) traz "Windows". */
export function textoSelagem(userAgent: string): TextoSelagem {
  return userAgent.includes("Windows") ? WINDOWS : SEM_SELAGEM;
}
