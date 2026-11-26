// Textos da proteção em repouso por plataforma (#39). O Windows sela a chave mestra por DPAPI e o
// Linux guarda no chaveiro do sistema (Secret Service, #40); fora deles (macOS) segue em arquivo 0600.

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

const LINUX: TextoSelagem = {
  pinSalvo: "PIN de 6 dígitos salvo (hash Argon2id, arquivo 0600).",
  definirPin: "Defina um código de 6 dígitos para bloquear o acesso às suas chaves",
  rodape:
    "Chave mestra no chaveiro do sistema (Secret Service), destravado no login: protege contra cópia do disco, não contra programas da sua sessão. Se o chaveiro for recriado, o cofre volta pelo backup .khvault.",
  selo: "KEYRING",
  cofre: "Cofre local (chave no chaveiro do sistema)",
};

const SEM_SELAGEM: TextoSelagem = {
  pinSalvo: "PIN de 6 dígitos salvo (hash Argon2id, arquivo 0600).",
  definirPin: "Defina um código de 6 dígitos para bloquear o acesso às suas chaves",
  rodape: "Chave mestra em arquivo 0600, sem selagem do sistema. Faça backup .khvault e cifre o disco.",
  selo: "0600",
  cofre: "Cofre local (chave 0600, sem selagem)",
};

/** Recebe o navigator.userAgent; o webview do Tauri no Windows (WebView2) traz "Windows", no Linux (WebKitGTK) traz "Linux". */
export function textoSelagem(userAgent: string): TextoSelagem {
  if (userAgent.includes("Windows")) return WINDOWS;
  return userAgent.includes("Linux") ? LINUX : SEM_SELAGEM;
}
