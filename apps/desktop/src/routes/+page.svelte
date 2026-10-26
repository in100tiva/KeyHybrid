<script lang="ts">
  import { invoke } from "@tauri-apps/api/core";
  import { onMount } from "svelte";

  interface KeySafeView {
    id: string;
    alias: string;
    provider: string;
    masked_key: string;
    virtual_token: string;
    default_model: string;
    active: boolean;
    updated: string;
  }

  const gatewayUrl = "http://127.0.0.1:8090";

  // Estado real do Gateway e Chaves
  let gatewayOnline = $state<boolean>(false);
  let keys = $state<KeySafeView[]>([]);
  let selectedKey = $derived(keys.find((k) => k.active) || keys[0] || null);

  // Inputs para adicionar nova chave na barra inferior (/Key | nome | sk-...)
  let newAlias = $state("");
  let newSecret = $state("");
  let newProvider = $state<"openrouter" | "gemini" | "anthropic">("openrouter");
  let newModel = $state("openrouter/auto");
  let isSaving = $state(false);
  let statusNotice = $state("");

  // Feedback de cópia rápida
  let copiedItem = $state<"" | "token" | "url" | "curl" | "masked">("");

  // Estado real do Bloqueio por PIN de 6 dígitos (Referência 3)
  let isLocked = $state(false);
  let isSettingPin = $state(false);
  let hasPinConfigured = $state(false);
  let pinDigits = $state(["", "", "", "", "", ""]);
  let activePinSlot = $state(0);
  let pinError = $state("");

  // Modal/Flip opcional para visualizar o Card Daybridge em tela cheia
  let showDaybridgeModal = $state(false);

  // Estado da Assistente Padrão "Sexta-Feira" (F.R.I.D.A.Y. + Lâmpada Xiaomi)
  // Regra: Só pode ser ativada/desativada após o usuário ter uma chave da OpenRouter cadastrada
  let hasOpenRouterKey = $derived(
    keys.some((k) => k.provider === "openrouter" || k.active)
  );
  let fridayEnabled = $state(false);
  let fridaySpeaking = $state(false);
  let fridayListening = $state(false);
  let fridayProcessing = $state(false);
  let fridayCommand = $state("");
  let fridayLastReply = $state("");
  let xiaomiBulbIp = $state("192.168.100.22");
  let dockMode = $state<"key" | "friday">("key");
  let recognitionRef: any = null;
  let currentAudio: HTMLAudioElement | null = null;
  let lastFridayEventId = 0;

  const PROVIDERS: Array<"openrouter" | "gemini" | "anthropic"> = [
    "openrouter",
    "gemini",
    "anthropic",
  ];

  const MODELS = [
    "openrouter/auto",
    "google/gemma-4-31b-it",
    "anthropic/claude-3.5-sonnet",
    "deepseek/deepseek-chat",
  ];

  let aliasInputEl: HTMLInputElement | undefined = $state();
  let pinContainerEl: HTMLDivElement | undefined = $state();

  // Estado da Animação de Entrada (Constelação sem fundo) e Efeito macOS Genie
  let appStage = $state<
    | "splash-constellation"
    | "genie-opening"
    | "ready"
    | "genie-minimizing"
    | "genie-restoring"
    | "genie-closing"
  >("splash-constellation");
  let wasMinimized = false;

  // Controles de janela minimalistas com Efeito macOS Genie (Distorção ao recolher/abrir)
  async function minimizeWin() {
    if (appStage === "genie-minimizing") return;
    appStage = "genie-minimizing";
    wasMinimized = true;
    setTimeout(async () => {
      await invoke("window_minimize");
    }, 470);
  }

  async function closeWin() {
    if (appStage === "genie-closing") return;
    appStage = "genie-closing";
    setTimeout(async () => {
      await invoke("window_close");
    }, 450);
  }

  async function startDrag(e: MouseEvent) {
    if ((e.target as HTMLElement).closest("button, input, a")) return;
    await invoke("window_start_dragging");
  }

  function showNotice(msg: string) {
    statusNotice = msg;
    setTimeout(() => {
      if (statusNotice === msg) statusNotice = "";
    }, 3200);
  }

  // Sincronizar status e chaves com o Gateway Go
  async function syncVault() {
    try {
      const res = await fetch(`${gatewayUrl}/api/keyhybrid/health`);
      if (res.ok) {
        gatewayOnline = true;
        const kRes = await fetch(`${gatewayUrl}/api/keyhybrid/keys`);
        if (kRes.ok) {
          keys = await kRes.json();
          return;
        }
      }
    } catch {
      // Fallback via Tauri invoke (que também auto-inicia o servidor Go se estiver fechado)
    }

    try {
      await invoke("check_gateway_health", { gatewayUrl });
      gatewayOnline = true;
      const list: KeySafeView[] = await invoke("list_vault_keys", { gatewayUrl });
      keys = list;
    } catch {
      gatewayOnline = false;
    }
  }

  // Salvar nova chave no Windows DPAPI + Gateway Go
  async function handleSaveKey() {
    if (isSaving) return;

    let rawAlias = newAlias.trim();
    let rawSecret = newSecret.trim();

    // Se o usuário colou a chave (sk-...) no primeiro campo por engano e deixou o segundo vazio
    if (!rawSecret && (rawAlias.startsWith("sk-") || rawAlias.length > 24)) {
      rawSecret = rawAlias;
      rawAlias = "";
    }

    if (!rawSecret) {
      showNotice("Cole sua chave secreta (ex: sk-or-v1-...) para salvar.");
      return;
    }

    // Se não digitou apelido, gera automaticamente pelo provedor (ex: openrouter)
    if (!rawAlias) {
      const base = newProvider;
      const exists = keys.some((k) => k.alias === base);
      rawAlias = exists ? `${base}-${keys.length + 1}` : base;
    }

    const alias = rawAlias.replace(/\s+/g, "-").toLowerCase();

    isSaving = true;
    try {
      // 1. Salvar direto no Gateway Go local (garante atualização instantânea)
      const resp = await fetch(`${gatewayUrl}/api/keyhybrid/keys`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          alias,
          provider: newProvider,
          secret_key: rawSecret,
          default_model: newModel,
        }),
      });

      if (!resp.ok) {
        const errData = await resp.text();
        throw new Error(errData || `HTTP ${resp.status}`);
      }

      // 2. Também espelhar no Cofre Nativo do Windows (DPAPI) via Rust
      try {
        await invoke("save_vault_key", {
          gatewayUrl,
          alias,
          provider: newProvider,
          secretKey: rawSecret,
          defaultModel: newModel,
        });
      } catch {
        // Ignora caso o binário Rust ainda não tenha sido reiniciado
      }

      newAlias = "";
      newSecret = "";
      await syncVault();
      showNotice(`Chave '${alias}' salva e ativa!`);
    } catch (err: any) {
      showNotice(`Erro: ${err.message || err}`);
    } finally {
      isSaving = false;
    }
  }

  // Ativar chave selecionada como padrão do Proxy
  async function handleActivateKey(k: KeySafeView) {
    if (k.active) return;
    try {
      await fetch(`${gatewayUrl}/api/keyhybrid/keys/${k.id}/activate`, {
        method: "POST",
      });
      await syncVault();
      showNotice(`'${k.alias}' definida como chave ativa.`);
    } catch (err: any) {
      showNotice(String(err));
    }
  }

  // Remover chave ativa ou específica
  async function handleDeleteKey(k: KeySafeView) {
    try {
      await fetch(`${gatewayUrl}/api/keyhybrid/keys/${k.id}`, {
        method: "DELETE",
      });
      try {
        await invoke("delete_vault_key", {
          gatewayUrl,
          id: k.id,
          alias: k.alias,
        });
      } catch {
        // Ignora se DPAPI já estiver limpo
      }
      await syncVault();
      showNotice(`Chave '${k.alias}' removida do cofre.`);
    } catch (err: any) {
      showNotice(String(err));
    }
  }

  // Alternar provedor no pill
  function cycleProvider() {
    const idx = PROVIDERS.indexOf(newProvider);
    newProvider = PROVIDERS[(idx + 1) % PROVIDERS.length];
  }

  // Alternar modelo padrão no pill
  function cycleModel() {
    const idx = MODELS.indexOf(newModel);
    newModel = MODELS[(idx + 1) % MODELS.length];
  }

  // Copiar dados seguros para usar no Antigravity / Terminal
  function copyText(type: "token" | "url" | "curl" | "masked", text: string) {
    navigator.clipboard.writeText(text);
    copiedItem = type;
    setTimeout(() => {
      if (copiedItem === type) copiedItem = "";
    }, 1800);
  }

  function getCurlSnippet(k: KeySafeView | null): string {
    const token = k ? k.virtual_token : "kh-default";
    const model = k?.default_model || newModel;
    return `curl http://127.0.0.1:8090/v1/chat/completions -H "Content-Type: application/json" -H "Authorization: Bearer ${token}" -d "{\\"model\\":\\"${model}\\",\\"messages\\":[{\\"role\\":\\"user\\",\\"content\\":\\"Olá\\"}]}"`;
  }

  // Lógica Real do PIN de 6 dígitos (Windows DPAPI)
  async function checkPinStatus() {
    try {
      hasPinConfigured = await invoke("has_vault_pin");
      if (hasPinConfigured) {
        isLocked = true;
        isSettingPin = false;
        resetPinBoxes();
      }
    } catch {
      hasPinConfigured = false;
    }
  }

  function resetPinBoxes() {
    pinDigits = ["", "", "", "", "", ""];
    activePinSlot = 0;
    pinError = "";
    setTimeout(() => pinContainerEl?.focus(), 50);
  }

  function openPinLockOrSetup() {
    if (hasPinConfigured) {
      isLocked = true;
      isSettingPin = false;
    } else {
      isLocked = true;
      isSettingPin = true;
    }
    resetPinBoxes();
  }

  async function removePinProtection() {
    await invoke("clear_vault_pin");
    hasPinConfigured = false;
    isLocked = false;
    isSettingPin = false;
    showNotice("PIN de bloqueio removido.");
  }

  async function handlePinKeydown(e: KeyboardEvent) {
    if (!isLocked) return;

    if (e.key >= "0" && e.key <= "9") {
      e.preventDefault();
      pinDigits[activePinSlot] = e.key;
      pinError = "";

      if (activePinSlot < 5) {
        activePinSlot += 1;
      } else {
        const fullPin = pinDigits.join("");
        if (isSettingPin) {
          try {
            await invoke("set_vault_pin", { pin: fullPin });
            hasPinConfigured = true;
            isSettingPin = false;
            isLocked = false;
            showNotice("PIN de 6 dígitos salvo no Windows DPAPI.");
          } catch (err: any) {
            pinError = String(err);
            resetPinBoxes();
          }
        } else {
          try {
            const ok: boolean = await invoke("verify_vault_pin", { pin: fullPin });
            if (ok) {
              isLocked = false;
              pinError = "";
            } else {
              pinError = "Código incorreto. Tente novamente.";
              resetPinBoxes();
            }
          } catch (err: any) {
            pinError = String(err);
            resetPinBoxes();
          }
        }
      }
    } else if (e.key === "Backspace") {
      e.preventDefault();
      if (pinDigits[activePinSlot] !== "") {
        pinDigits[activePinSlot] = "";
      } else if (activePinSlot > 0) {
        activePinSlot -= 1;
        pinDigits[activePinSlot] = "";
      }
    } else if (e.key === "Escape" && isSettingPin) {
      isLocked = false;
      isSettingPin = false;
    }
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // ASSISTENTE PADRÃO: SEXTA-FEIRA (F.R.I.D.A.Y. + LÂMPADA INTELIGENTE XIAOMI)
  // ═══════════════════════════════════════════════════════════════════════════
  function playFridayAudio(base64Mp3: string) {
    if (!base64Mp3) return;
    try {
      if (currentAudio) {
        currentAudio.pause();
        currentAudio = null;
      }
      const audio = new Audio(`data:audio/mp3;base64,${base64Mp3}`);
      currentAudio = audio;
      fridaySpeaking = true;
      audio.onended = () => {
        fridaySpeaking = false;
      };
      audio.onerror = () => {
        fridaySpeaking = false;
      };
      audio.play().catch(() => {
        fridaySpeaking = false;
      });
    } catch {
      fridaySpeaking = false;
    }
  }

  function startFridayWakeListener() {
    const SpeechRec =
      (window as any).SpeechRecognition ||
      (window as any).webkitSpeechRecognition;
    if (!SpeechRec) return;

    try {
      if (recognitionRef) {
        recognitionRef.stop();
      }
      const rec = new SpeechRec();
      rec.lang = "pt-BR";
      rec.continuous = true;
      rec.interimResults = false;

      rec.onstart = () => {
        fridayListening = true;
      };

      rec.onresult = (event: any) => {
        const lastIdx = event.results.length - 1;
        const transcript = String(
          event.results[lastIdx][0].transcript || ""
        ).trim();
        if (!transcript) return;

        const norm = transcript
          .toLowerCase()
          .normalize("NFD")
          .replace(/[\u0300-\u036f]/g, "");

        // Atende quando o usuário chama por "Sexta-feira" / "Sexta feira"
        if (norm.includes("sexta-feira") || norm.includes("sexta feira") || norm.includes("sextafeira")) {
          fridayCommand = transcript;
          sendFridayCommand(transcript);
        }
      };

      rec.onerror = () => {
        fridayListening = false;
      };

      rec.onend = () => {
        fridayListening = false;
        if (fridayEnabled && hasOpenRouterKey) {
          setTimeout(() => {
            try {
              rec.start();
            } catch {}
          }, 600);
        }
      };

      recognitionRef = rec;
      rec.start();
    } catch {
      fridayListening = false;
    }
  }

  function stopFridayWakeListener() {
    fridayListening = false;
    if (recognitionRef) {
      try {
        recognitionRef.onend = null;
        recognitionRef.stop();
      } catch {}
      recognitionRef = null;
    }
  }

  async function toggleFriday() {
    if (!hasOpenRouterKey) {
      showNotice("Adicione sua chave OpenRouter para ativar a Sexta-Feira.");
      return;
    }

    fridayEnabled = !fridayEnabled;
    try {
      await fetch(`${gatewayUrl}/api/keyhybrid/friday/state`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ enabled: fridayEnabled }),
      });
    } catch {}
    if (fridayEnabled) {
      dockMode = "friday";
      fridayListening = true;
      showNotice("Sexta-Feira ativada! Diga 'Sexta-feira...'");
      startFridayWakeListener();
      try {
        const resp = await fetch(`${gatewayUrl}/api/keyhybrid/friday/assist`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ greeting: true, bulb_ip: xiaomiBulbIp }),
        });
        if (resp.ok) {
          const data = await resp.json();
          fridayLastReply = data.reply || "";
          if (data.audio_base64) {
            playFridayAudio(data.audio_base64);
          }
        }
      } catch {}
    } else {
      dockMode = "key";
      stopFridayWakeListener();
      if (currentAudio) {
        currentAudio.pause();
        fridaySpeaking = false;
      }
      showNotice("Sexta-Feira desativada.");
    }
  }

  async function pollFridayVoiceEvents() {
    if (!hasOpenRouterKey) return;
    try {
      const resp = await fetch(`${gatewayUrl}/api/keyhybrid/friday/state`);
      if (!resp.ok) return;
      const st = await resp.json();
      if (typeof st.enabled === "boolean") {
        fridayEnabled = st.enabled;
        fridayListening = st.enabled;
      }
      const ev = st.event;
      if (ev && ev.event_id && ev.event_id > lastFridayEventId) {
        if (lastFridayEventId !== 0) {
          fridayLastReply = ev.reply || "";
          showNotice(`🎙️ "${ev.text}" ➔ ${ev.reply}`);
          fridaySpeaking = true;
          setTimeout(() => {
            fridaySpeaking = false;
          }, 3600);
        }
        lastFridayEventId = ev.event_id;
      }
    } catch {}
  }

  async function sendFridayCommand(overrideText?: string) {
    if (!hasOpenRouterKey) {
      showNotice("Requer chave da OpenRouter no cofre.");
      return;
    }
    const text = (overrideText ?? fridayCommand).trim();
    if (!text || fridayProcessing) return;

    fridayProcessing = true;
    showNotice("Sexta-Feira processando...");
    try {
      const resp = await fetch(`${gatewayUrl}/api/keyhybrid/friday/assist`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          text,
          bulb_ip: xiaomiBulbIp.trim(),
        }),
      });
      if (!resp.ok) {
        const errTxt = await resp.text();
        throw new Error(errTxt);
      }
      const data = await resp.json();
      if (data.event_id) {
        lastFridayEventId = data.event_id;
      }
      fridayLastReply = data.reply || "";
      if (data.bulb_ip && !xiaomiBulbIp) {
        xiaomiBulbIp = data.bulb_ip;
      }
      if (data.audio_base64) {
        playFridayAudio(data.audio_base64);
      }
      fridayCommand = "";
      showNotice(data.reply || "Comando concluído.");
    } catch (err: any) {
      showNotice(`Erro Sexta-Feira: ${err.message || err}`);
    } finally {
      fridayProcessing = false;
    }
  }

  $effect(() => {
    if (!hasOpenRouterKey && fridayEnabled) {
      fridayEnabled = false;
      dockMode = "key";
      stopFridayWakeListener();
    }
  });

  onMount(() => {
    syncVault();
    checkPinStatus();

    // 1. Fase inicial: apenas a constelação flutuando sem fundo preto (1.35s)
    // 2. Fase Genie: a janela abre com a distorção estilo macOS (0.68s)
    const tOpen = setTimeout(() => {
      appStage = "genie-opening";
      setTimeout(() => {
        if (appStage === "genie-opening") appStage = "ready";
      }, 680);
    }, 1350);

    // Quando o usuário restaura a janela da barra de tarefas após minimizar, dispara o Genie Restore
    const handleRestore = () => {
      if (wasMinimized && !document.hidden) {
        wasMinimized = false;
        appStage = "genie-restoring";
        setTimeout(() => {
          if (appStage === "genie-restoring") appStage = "ready";
        }, 650);
      }
    };

    window.addEventListener("focus", handleRestore);
    document.addEventListener("visibilitychange", handleRestore);

    const timer = setInterval(syncVault, 6000);
    const voiceTimer = setInterval(pollFridayVoiceEvents, 650);
    return () => {
      clearTimeout(tOpen);
      clearInterval(timer);
      clearInterval(voiceTimer);
      window.removeEventListener("focus", handleRestore);
      document.removeEventListener("visibilitychange", handleRestore);
    };
  });
</script>

<!-- FILTRO SVG DE DISTORÇÃO LÍQUIDA ESTILO MACOS GENIE -->
<svg class="genie-svg-defs" width="0" height="0" aria-hidden="true">
  <defs>
    <filter id="macos-genie-warp" x="-20%" y="-20%" width="140%" height="140%">
      <feTurbulence type="fractalNoise" baseFrequency="0.012 0.04" numOctaves="1" result="warp" />
      <feDisplacementMap in="SourceGraphic" in2="warp" scale="18" xChannelSelector="R" yChannelSelector="G" />
    </filter>
  </defs>
</svg>

<!-- ANIMAÇÃO DE ENTRADA: CONSTELAÇÃO PURA SEM FUNDO PRETO -->
{#if appStage === "splash-constellation"}
  <div class="constellation-splash-stage">
    <div class="pure-constellation">
      <div class="pure-cosmic-halo"></div>
      <div class="pure-orbit-assembly">
        <div class="pure-light-streak"></div>
        <div class="pure-orbital-ring"></div>
        <div class="pure-core-star"></div>
        <span class="star-speck s1"></span>
        <span class="star-speck s2"></span>
        <span class="star-speck s3"></span>
      </div>
    </div>
  </div>
{/if}

<!-- VIEWPORT 3D COM EFEITO MACOS GENIE (RECOLHER / ABRIR COM DISTORÇÃO) -->
<div class="genie-viewport">
  <div class="window-frame stage-{appStage}">
  <!-- BARRA DE TOPO MINIMALISTA (ARRASTÁVEL + CONTROLES DISCRETOS) -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <header class="window-bar" onmousedown={startDrag} data-tauri-drag-region>
    <div class="gateway-indicator" title={gatewayOnline ? "Gateway Go Online em 127.0.0.1:8090" : "Gateway Offline"}>
      <span class="gw-dot {gatewayOnline ? 'online' : 'offline'}"></span>
      <span class="gw-label">{gatewayOnline ? "127.0.0.1:8090" : "gateway offline"}</span>
    </div>

    <div class="win-actions">
      <button class="win-btn" onclick={minimizeWin} aria-label="Minimizar">
        <svg width="10" height="2" viewBox="0 0 10 2" fill="none">
          <path d="M1 1H9" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
        </svg>
      </button>
      <button class="win-btn close" onclick={closeWin} aria-label="Fechar">
        <svg width="10" height="10" viewBox="0 0 10 10" fill="none">
          <path d="M1.5 1.5L8.5 8.5M8.5 1.5L1.5 8.5" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
        </svg>
      </button>
    </div>
  </header>

  {#if isLocked}
    <!-- ═══════════════════════════════════════════════════════════════════ -->
    <!-- TELA DE BLOQUEIO 6 DÍGITOS REAL (FIEL À REFERÊNCIA 3)              -->
    <!-- ═══════════════════════════════════════════════════════════════════ -->
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <section
      class="lock-screen"
      bind:this={pinContainerEl}
      tabindex="0"
      onkeydown={handlePinKeydown}
    >
      <div class="padlock-stage">
        <div class="ambient-blue-glow"></div>
        <div class="padlock-3d">
          <div class="padlock-shackle"></div>
          <div class="padlock-body">
            <div class="padlock-emblem">
              <svg width="26" height="26" viewBox="0 0 24 24" fill="none">
                <path d="M8 4L16 12L8 20" stroke="#1e293b" stroke-width="2.2" stroke-linecap="round" />
                <path d="M16 4L8 12L16 20" stroke="#1e293b" stroke-width="2.2" stroke-linecap="round" />
                <rect x="6" y="6" width="12" height="12" rx="2" stroke="#1e293b" stroke-width="1.8" />
              </svg>
            </div>
          </div>
          <div class="padlock-fade"></div>
        </div>
      </div>

      <h2 class="lock-title">
        {isSettingPin ? "Create 6 digit code" : "6 digit code"}
      </h2>
      <p class="lock-subtitle">
        {isSettingPin
          ? "Defina um código de 6 dígitos para proteger suas chaves no DPAPI"
          : "Digite o código de verificação para desbloquear o KeyHybrid"}
      </p>

      <div class="otp-row">
        {#each pinDigits as digit, idx}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <div
            class="otp-slot {activePinSlot === idx ? 'active' : ''}"
            onclick={() => {
              activePinSlot = idx;
              pinContainerEl?.focus();
            }}
          >
            <span class="otp-char">{digit}</span>
            {#if activePinSlot === idx}
              <span class="otp-caret"></span>
            {/if}
          </div>
        {/each}
      </div>

      {#if pinError}
        <p class="lock-error">{pinError}</p>
      {:else if isSettingPin}
        <p class="lock-helper">
          Pressione números de 0 a 9 • <button class="inline-link" onclick={() => { isLocked = false; isSettingPin = false; }}>Cancelar</button>
        </p>
      {:else}
        <p class="lock-helper">
          Cofre protegido via Windows DPAPI
        </p>
      {/if}
    </section>

  {:else if showDaybridgeModal && selectedKey}
    <!-- ═══════════════════════════════════════════════════════════════════ -->
    <!-- CARD DE TOKEN VIRTUAL (FIEL À REFERÊNCIA 1 - DAYBRIDGE)            -->
    <!-- ═══════════════════════════════════════════════════════════════════ -->
    <section class="daybridge-stage">
      <div class="purple-orbit-lines"></div>

      <div class="daybridge-outer">
        <div class="daybridge-inner">
          <div class="db-top-meta">
            <span><small>PROVIDER</small> {selectedKey.provider.toUpperCase()}</span>
            <span><small>KEY</small> {selectedKey.masked_key}</span>
            <span><small>PWR</small> DPAPI</span>
          </div>

          <div class="db-center-block">
            <div class="db-sparkle-icon">✦</div>
            <div class="db-invite-label">VIRTUAL ACCESS TOKEN</div>
            <div class="db-invite-code">{selectedKey.virtual_token.toUpperCase()}</div>
          </div>

          <div class="db-bottom-meta">
            <span>ALIAS</span> <strong>{selectedKey.alias.toUpperCase()}</strong>
          </div>
        </div>

        <div class="daybridge-footer">
          <button
            class="db-copy-action"
            onclick={() => copyText("token", selectedKey.virtual_token)}
          >
            {copiedItem === "token" ? "✓ Token copiado" : "Copiar Token Seguro"}
          </button>
          <button class="db-back-btn" onclick={() => (showDaybridgeModal = false)}>
            Voltar ao painel
          </button>
        </div>
      </div>
    </section>

  {:else}
    <!-- ═══════════════════════════════════════════════════════════════════ -->
    <!-- TELA PRINCIPAL (FIEL À REFERÊNCIA 2 - PLAN WISE + ANIMAÇÃO PINTEREST) -->
    <!-- ═══════════════════════════════════════════════════════════════════ -->
    <section class="planwise-stage">
      <!-- ÍCONE CENTRAL COM A ANIMAÇÃO DO PINTEREST (ESFERA ESCURA + FEIXE LUMINOSO) -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="hero-header" onmousedown={startDrag}>
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <div
          class="orbital-squircle {fridaySpeaking ? 'friday-speaking' : fridayEnabled ? 'friday-online' : ''}"
          title="Clique para rever a animação de abertura da Constelação + Genie"
          onclick={() => {
            appStage = "splash-constellation";
            setTimeout(() => {
              appStage = "genie-opening";
              setTimeout(() => {
                if (appStage === "genie-opening") appStage = "ready";
              }, 680);
            }, 1350);
          }}
        >
          <div class="sphere-shadow"></div>
          <div class="orbital-assembly">
            <div class="light-streak"></div>
            <div class="orbital-ring"></div>
            <div class="core-star"></div>
          </div>
        </div>

        <h1 class="main-title">KeyHybrid</h1>
        <p class="main-subtitle">
          {#if fridayEnabled}
            {fridayLastReply || "Sexta-Feira na escuta — diga \"Sexta-feira, acende a luz\" ou use a barra abaixo"}
          {:else}
            Armazene suas chaves e use qualquer modelo no terminal ou Antigravity sem expor credenciais
          {/if}
        </p>
      </div>

      <!-- CONTAINER INTERNO DUPLO (EXATAMENTE COMO STEP 1 / STEP 2 DO PLAN WISE) -->
      <div class="inner-board">
        <!-- COLUNA 1: CHAVES SALVAS -->
        <div class="board-col left-col">
          <div class="col-step-row">
            <span class="step-tag">Step 1</span>
          </div>
          <div class="col-heading">
            <span class="dots-grid">⠿</span>
            <span>Suas chaves</span>
          </div>

          <div class="items-list">
            {#if keys.length === 0}
              <div class="empty-keys">
                <span>Nenhuma chave salva.</span>
                <small>Adicione na barra abaixo ↓</small>
              </div>
            {:else}
              {#each keys as k (k.id)}
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <div
                  class="key-row {k.active ? 'selected' : ''}"
                  onclick={() => handleActivateKey(k)}
                >
                  <div class="key-row-left">
                    {#if k.active}
                      <span class="check-lime">✓</span>
                    {:else}
                      <span class="prov-badge {k.provider}"></span>
                    {/if}
                    <span class="key-alias">{k.alias}</span>
                  </div>

                  <div class="key-row-right">
                    {#if k.active}
                      <span class="team-pill">ACTIVE</span>
                    {:else}
                      <span class="linked-tag">{k.provider}</span>
                    {/if}
                    <button
                      class="del-micro"
                      onclick={(e) => {
                        e.stopPropagation();
                        handleDeleteKey(k);
                      }}
                      title="Remover chave"
                    >
                      ×
                    </button>
                  </div>
                </div>
              {/each}
            {/if}
          </div>

          <div class="col-footer-meta">
            <span class="meta-square"></span>
            <span class="meta-text">
              {selectedKey ? `${selectedKey.masked_key}` : "Cofre Windows DPAPI"}
            </span>
          </div>
        </div>

        <!-- DIVISÓRIA VERTICAL -->
        <div class="board-divider"></div>

        <!-- COLUNA 2: ACESSO EXTERNO (COMO USAR SEM VAZAR A CHAVE) -->
        <div class="board-col right-col">
          <div class="col-step-row">
            <span class="step-tag">Step 2</span>
          </div>
          <div class="col-heading">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="#71717a" stroke-width="2">
              <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
              <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
            </svg>
            <span>Acesso sem vazamento</span>
          </div>

          <div class="access-list">
            <!-- Item 1: Copiar Token Virtual -->
            <button
              class="access-row {copiedItem === 'token' ? 'active-row' : ''}"
              onclick={() =>
                selectedKey && copyText("token", selectedKey.virtual_token)}
              disabled={!selectedKey}
            >
              <span class="avatar-badge purple">✦</span>
              <span class="access-label">
                {selectedKey ? selectedKey.virtual_token : "kh-token"}
              </span>
              <span class="access-check">
                {copiedItem === "token" ? "✓" : "Copiar"}
              </span>
            </button>

            <!-- Item 2: Copiar Base URL OpenAI-compatible -->
            <button
              class="access-row {copiedItem === 'url' ? 'active-row' : ''}"
              onclick={() => copyText("url", "http://127.0.0.1:8090/v1")}
            >
              <span class="avatar-badge blue">⇄</span>
              <span class="access-label">127.0.0.1:8090/v1</span>
              <span class="access-check">
                {copiedItem === "url" ? "✓" : "URL"}
              </span>
            </button>

            <!-- Item 3: Copiar Comando cURL pronto para Antigravity / Terminal -->
            <button
              class="access-row {copiedItem === 'curl' ? 'active-row' : ''}"
              onclick={() => copyText("curl", getCurlSnippet(selectedKey))}
            >
              <span class="avatar-badge amber">&gt;_</span>
              <span class="access-label">Comando p/ IA / cURL</span>
              <span class="access-check">
                {copiedItem === "curl" ? "✓" : "Copiar"}
              </span>
            </button>

            <!-- Item 4: Abrir Card Daybridge do Token -->
            <button
              class="access-row"
              onclick={() => {
                if (selectedKey) showDaybridgeModal = true;
              }}
              disabled={!selectedKey}
            >
              <span class="avatar-badge dark">◈</span>
              <span class="access-label">Ver Card do Token</span>
              <span class="access-check">Abrir</span>
            </button>
          </div>
        </div>
      </div>

      <!-- FAIXA DE 3 PILLS ABAIXO DO QUADRO (EXATAMENTE COMO NO PLAN WISE) -->
      <div class="Quick-pills-row">
        <button class="config-pill" onclick={cycleProvider} title="Clique para alternar o provedor da nova chave">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="3" y="4" width="18" height="16" rx="3" />
            <path d="M3 10h18" />
          </svg>
          <span>Provedor</span>
          <small>{newProvider}</small>
          <span class="pill-arrow">⌄</span>
        </button>

        <button
          class="config-pill {fridayEnabled ? 'friday-pill-active' : ''} {!hasOpenRouterKey ? 'friday-pill-locked' : ''}"
          onclick={toggleFriday}
          disabled={!hasOpenRouterKey}
          title={hasOpenRouterKey
            ? "Ativar/Desativar Assistente de Voz Sexta-Feira (F.R.I.D.A.Y.)"
            : "Salve uma chave da OpenRouter primeiro para ativar a Sexta-Feira"}
        >
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z" />
            <path d="M19 10v2a7 7 0 0 1-14 0v-2" />
            <line x1="12" y1="19" x2="12" y2="23" />
          </svg>
          <span>Sexta-Feira</span>
          <small>
            {#if !hasOpenRouterKey}
              bloqueada
            {:else if fridaySpeaking}
              falando...
            {:else if fridayEnabled}
              ativa
            {:else}
              off
            {/if}
          </small>
        </button>

        <button class="config-pill" onclick={openPinLockOrSetup} title="Bloquear ou configurar PIN de 6 dígitos">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="4" y="11" width="16" height="10" rx="2" />
            <path d="M8 11V7a4 4 0 0 1 8 0v4" />
          </svg>
          <span>{hasPinConfigured ? "Bloquear" : "Criar PIN"}</span>
          <small>6 dígitos</small>
        </button>
      </div>

      <!-- RODAPÉ INTEGRADO (TOOLBAR + DOCK COM ORBE VERDE LUMINOSA DO PLAN WISE) -->
      <div class="bottom-dock-container">
        <div class="dock-toolbar">
          <button
            class="tool-item {dockMode === 'key' ? 'active-tool' : ''}"
            onclick={() => {
              dockMode = "key";
              setTimeout(() => aliasInputEl?.focus(), 40);
            }}
          >
            <span>+</span> Nova chave
          </button>
          {#if hasOpenRouterKey}
            <span class="tool-sep">|</span>
            <button
              class="tool-item {dockMode === 'friday' ? 'active-tool' : ''}"
              onclick={() => {
                dockMode = "friday";
                if (!fridayEnabled) toggleFriday();
              }}
            >
              <span>🎙️</span> Sexta-Feira {fridayListening ? "• ouvindo" : ""}
            </button>
          {/if}
          <span class="tool-sep">|</span>
          <button class="tool-item" onclick={syncVault}>
            <span>⟳</span> Sincronizar
          </button>
          {#if hasPinConfigured}
            <span class="tool-sep">|</span>
            <button class="tool-item" onclick={removePinProtection}>
              <span>🔓</span> Remover PIN
            </button>
          {/if}
          {#if statusNotice}
            <span class="notice-inline">{statusNotice}</span>
          {/if}
        </div>

        {#if dockMode === "friday" && hasOpenRouterKey}
          <div class="dock-input-bar">
            <div class="lime-orb {fridaySpeaking ? 'speaking-orb' : ''}"></div>

            <span class="cmd-prefix">/F.R.I.D.A.Y</span>
            <span class="cmd-pipe">|</span>

            <input
              type="text"
              class="input-alias"
              placeholder="IP Xiaomi (opcional)"
              bind:value={xiaomiBulbIp}
              title="IP local da lâmpada Xiaomi (ex: 192.168.100.55) com Controle de LAN ativo"
            />

            <input
              type="text"
              class="input-secret"
              placeholder="Diga &quot;Sexta-feira, acende a luz&quot; ou digite aqui..."
              bind:value={fridayCommand}
              onkeydown={(e) => {
                if (e.key === "Enter") sendFridayCommand();
              }}
            />

            <button
              class="dock-send-btn"
              onclick={() => sendFridayCommand()}
              disabled={fridayProcessing || !fridayCommand.trim()}
              title="Enviar comando para a Sexta-Feira"
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <line x1="22" y1="2" x2="11" y2="13"></line>
                <polygon points="22 2 15 22 11 13 2 9 22 2"></polygon>
              </svg>
            </button>
          </div>
        {:else}
          <div class="dock-input-bar">
            <div class="lime-orb"></div>

            <span class="cmd-prefix">/Key</span>
            <span class="cmd-pipe">|</span>

            <input
              bind:this={aliasInputEl}
              type="text"
              class="input-alias"
              placeholder="nome (ex: padrao)"
              bind:value={newAlias}
              onkeydown={(e) => {
                if (e.key === "Enter") handleSaveKey();
              }}
            />

            <input
              type="password"
              class="input-secret"
              placeholder="cole a chave sk-or-v1-..."
              bind:value={newSecret}
              onkeydown={(e) => {
                if (e.key === "Enter") handleSaveKey();
              }}
            />

            <button
              class="dock-send-btn"
              onclick={handleSaveKey}
              disabled={isSaving || (!newAlias.trim() && !newSecret.trim())}
              title="Salvar chave no Cofre"
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <line x1="22" y1="2" x2="11" y2="13"></line>
                <polygon points="22 2 15 22 11 13 2 9 22 2"></polygon>
              </svg>
            </button>
          </div>
        {/if}
      </div>
    </section>
  {/if}
  </div>
</div>

<style>
  :global(html, body) {
    margin: 0;
    padding: 0;
    background: transparent;
    color: #f4f4f5;
    font-family: "Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    overflow: hidden;
    user-select: none;
    -webkit-font-smoothing: antialiased;
  }

  .genie-svg-defs {
    position: absolute;
    width: 0;
    height: 0;
    pointer-events: none;
  }

  /* ═════════════════════════════════════════════════════════════════ */
  /* ANIMAÇÃO DE ENTRADA: CONSTELAÇÃO PURA FLUTUANDO SEM FUNDO PRETO   */
  /* ═════════════════════════════════════════════════════════════════ */
  .constellation-splash-stage {
    position: fixed;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    z-index: 9999;
    pointer-events: none;
  }

  .pure-constellation {
    width: 112px;
    height: 112px;
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    animation: constellationIntroFlight 1.35s cubic-bezier(0.22, 1, 0.36, 1) forwards;
  }

  .pure-cosmic-halo {
    position: absolute;
    width: 82px;
    height: 82px;
    border-radius: 50%;
    background: radial-gradient(
      circle,
      rgba(147, 197, 253, 0.28) 0%,
      rgba(99, 102, 241, 0.14) 48%,
      transparent 74%
    );
    filter: blur(8px);
    animation: haloBreathe 1.1s ease-in-out infinite alternate;
  }

  .pure-orbit-assembly {
    position: relative;
    width: 96px;
    height: 96px;
    display: flex;
    align-items: center;
    justify-content: center;
    transform: rotate(-38deg);
    animation: pureOrbitSpinIn 1.35s cubic-bezier(0.16, 1, 0.3, 1) forwards;
  }

  .pure-light-streak {
    position: absolute;
    width: 92px;
    height: 16px;
    border-radius: 50%;
    background: radial-gradient(
      ellipse at center,
      rgba(191, 219, 254, 0.72) 0%,
      rgba(96, 165, 250, 0.32) 45%,
      transparent 78%
    );
    filter: blur(3px);
  }

  .pure-orbital-ring {
    position: absolute;
    width: 86px;
    height: 22px;
    border-radius: 50%;
    border: 2.2px solid rgba(240, 249, 255, 0.96);
    box-shadow:
      0 0 14px rgba(186, 230, 253, 0.95),
      0 0 28px rgba(96, 165, 250, 0.65),
      inset 0 0 10px rgba(186, 230, 253, 0.75);
  }

  .pure-core-star {
    width: 24px;
    height: 24px;
    border-radius: 50%;
    background: radial-gradient(
      circle,
      #ffffff 0%,
      #f0f9ff 58%,
      rgba(147, 197, 253, 0.9) 100%
    );
    box-shadow:
      0 0 16px 5px rgba(255, 255, 255, 0.98),
      0 0 34px 12px rgba(147, 197, 253, 0.72);
  }

  .star-speck {
    position: absolute;
    border-radius: 50%;
    background: #ffffff;
    box-shadow: 0 0 8px #93c5fd;
  }

  .star-speck.s1 {
    width: 3px;
    height: 3px;
    top: 14px;
    left: 20px;
    animation: twinkleSpeck 0.7s ease-in-out infinite alternate;
  }

  .star-speck.s2 {
    width: 2.5px;
    height: 2.5px;
    bottom: 16px;
    right: 18px;
    animation: twinkleSpeck 0.9s ease-in-out 0.2s infinite alternate;
  }

  .star-speck.s3 {
    width: 2px;
    height: 2px;
    top: 22px;
    right: 26px;
    animation: twinkleSpeck 0.6s ease-in-out 0.35s infinite alternate;
  }

  @keyframes constellationIntroFlight {
    0% {
      opacity: 0;
      transform: scale(0.25) translateY(24px);
      filter: blur(8px);
    }
    28% {
      opacity: 1;
      transform: scale(1.12) translateY(0px);
      filter: blur(0px);
    }
    70% {
      opacity: 1;
      transform: scale(1) translateY(0px);
    }
    100% {
      opacity: 0.95;
      transform: translateY(-236px) scale(0.54);
    }
  }

  @keyframes pureOrbitSpinIn {
    0% {
      transform: rotate(-145deg) scaleX(0.4);
    }
    45% {
      transform: rotate(-32deg) scaleX(1.06);
    }
    100% {
      transform: rotate(-38deg) scaleX(1);
    }
  }

  @keyframes haloBreathe {
    from {
      transform: scale(0.88);
      opacity: 0.65;
    }
    to {
      transform: scale(1.22);
      opacity: 1;
    }
  }

  @keyframes twinkleSpeck {
    from {
      opacity: 0.25;
      transform: scale(0.7);
    }
    to {
      opacity: 1;
      transform: scale(1.35);
    }
  }

  /* ═════════════════════════════════════════════════════════════════ */
  /* VIEWPORT 3D + EFEITO MACOS GENIE DE DISTORÇÃO DE JANELA           */
  /* ═════════════════════════════════════════════════════════════════ */
  .genie-viewport {
    width: 100vw;
    height: 100vh;
    perspective: 920px;
    perspective-origin: 50% 88%;
    display: flex;
    align-items: center;
    justify-content: center;
    background: transparent;
  }

  /* MOLDURA PRINCIPAL DO CARD (A PRÓPRIA JANELA) */
  .window-frame {
    width: calc(100vw - 4px);
    height: calc(100vh - 4px);
    margin: 2px;
    box-sizing: border-box;
    background:
      radial-gradient(ellipse at 50% 0%, rgba(255, 255, 255, 0.055) 0%, transparent 58%),
      #111113;
    border: 1px solid rgba(255, 255, 255, 0.09);
    border-radius: 24px;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    position: relative;
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.08);
    transform-origin: 50% 12%;
    will-change: transform, opacity, filter, clip-path;
  }

  /* Enquanto a constelação flutua sozinha, o card fica invisível (sem fundo preto) */
  .window-frame.stage-splash-constellation {
    opacity: 0;
    pointer-events: none;
    transform: translateY(-210px) scaleX(0.08) scaleY(0.04);
  }

  /* Abertura suave emergindo da constelação com distorção Genie */
  .window-frame.stage-genie-opening {
    transform-origin: 50% 10%;
    animation: macGenieUnfoldFromTop 0.68s cubic-bezier(0.19, 1, 0.22, 1) forwards;
  }

  /* Minimizar: suga e distorce a janela em funil para a barra inferior (macOS Genie) */
  .window-frame.stage-genie-minimizing {
    transform-origin: 50% 100%;
    pointer-events: none;
    animation: macGenieSuckDown 0.47s cubic-bezier(0.55, 0.06, 0.68, 0.19) forwards;
  }

  /* Restaurar da barra de tarefas: expande do funil inferior com distorção Genie */
  .window-frame.stage-genie-restoring {
    transform-origin: 50% 100%;
    animation: macGenieRestoreUp 0.64s cubic-bezier(0.19, 1, 0.22, 1) forwards;
  }

  /* Fechar: recolhe em direção à constelação central */
  .window-frame.stage-genie-closing {
    transform-origin: 50% 16%;
    pointer-events: none;
    animation: macGenieCloseCollapse 0.44s cubic-bezier(0.55, 0.06, 0.68, 0.19) forwards;
  }

  @keyframes macGenieUnfoldFromTop {
    0% {
      opacity: 0;
      transform: translateY(-195px) scaleX(0.11) scaleY(0.06) rotateX(42deg) skewX(-6deg);
      clip-path: polygon(42% 0%, 58% 0%, 68% 100%, 32% 100%);
      filter: url(#macos-genie-warp) blur(6px) brightness(1.4);
      border-radius: 80px;
    }
    38% {
      opacity: 0.92;
      transform: translateY(-45px) scaleX(0.52) scaleY(0.74) rotateX(20deg) skewX(3deg);
      clip-path: polygon(16% 0%, 84% 0%, 100% 100%, 0% 100%);
      filter: url(#macos-genie-warp) blur(1.5px) brightness(1.12);
      border-radius: 42px;
    }
    76% {
      opacity: 1;
      transform: translateY(4px) scaleX(1.015) scaleY(1.01) rotateX(-3deg) skewX(0deg);
      clip-path: polygon(0% 0%, 100% 0%, 100% 100%, 0% 100%);
      filter: none;
      border-radius: 24px;
    }
    100% {
      opacity: 1;
      transform: none;
      clip-path: polygon(0% 0%, 100% 0%, 100% 100%, 0% 100%);
      filter: none;
      border-radius: 24px;
    }
  }

  @keyframes macGenieSuckDown {
    0% {
      opacity: 1;
      transform: translateY(0) scaleX(1) scaleY(1) rotateX(0deg) skewX(0deg);
      clip-path: polygon(0% 0%, 100% 0%, 100% 100%, 0% 100%);
      filter: none;
    }
    35% {
      opacity: 0.96;
      transform: translateY(55px) scaleX(0.76) scaleY(0.84) rotateX(-22deg) skewX(-5deg);
      clip-path: polygon(0% 0%, 100% 0%, 72% 100%, 28% 100%);
      filter: url(#macos-genie-warp) blur(1px);
    }
    72% {
      opacity: 0.65;
      transform: translateY(210px) scaleX(0.24) scaleY(0.36) rotateX(-38deg) skewX(8deg);
      clip-path: polygon(12% 0%, 88% 0%, 56% 100%, 44% 100%);
      filter: url(#macos-genie-warp) blur(4px);
    }
    100% {
      opacity: 0;
      transform: translateY(360px) scaleX(0.04) scaleY(0.03) rotateX(-48deg) skewX(12deg);
      clip-path: polygon(45% 0%, 55% 0%, 51% 100%, 49% 100%);
      filter: blur(8px);
    }
  }

  @keyframes macGenieRestoreUp {
    0% {
      opacity: 0;
      transform: translateY(350px) scaleX(0.05) scaleY(0.04) rotateX(-46deg) skewX(-10deg);
      clip-path: polygon(44% 0%, 56% 0%, 51% 100%, 49% 100%);
      filter: url(#macos-genie-warp) blur(8px);
    }
    42% {
      opacity: 0.92;
      transform: translateY(85px) scaleX(0.58) scaleY(0.82) rotateX(-20deg) skewX(4deg);
      clip-path: polygon(4% 0%, 96% 0%, 76% 100%, 24% 100%);
      filter: url(#macos-genie-warp) blur(1.5px);
    }
    78% {
      opacity: 1;
      transform: translateY(-5px) scaleX(1.015) scaleY(1.01) rotateX(3deg) skewX(0deg);
      clip-path: polygon(0% 0%, 100% 0%, 100% 100%, 0% 100%);
      filter: none;
    }
    100% {
      opacity: 1;
      transform: none;
      clip-path: polygon(0% 0%, 100% 0%, 100% 100%, 0% 100%);
      filter: none;
    }
  }

  @keyframes macGenieCloseCollapse {
    0% {
      opacity: 1;
      transform: scale(1) rotateX(0deg);
      filter: none;
    }
    45% {
      opacity: 0.8;
      transform: translateY(-60px) scaleX(0.45) scaleY(0.52) rotateX(26deg) skewX(-6deg);
      clip-path: polygon(22% 0%, 78% 0%, 95% 100%, 5% 100%);
      filter: url(#macos-genie-warp) blur(3px) brightness(1.3);
    }
    100% {
      opacity: 0;
      transform: translateY(-210px) scaleX(0.04) scaleY(0.04) rotateX(45deg);
      clip-path: polygon(48% 0%, 52% 0%, 52% 100%, 48% 100%);
      filter: blur(10px) brightness(2);
    }
  }

  /* BARRA DE JANELA MINIMALISTA (SEM BOTÕES CLÁSSICOS DO WINDOWS) */
  .window-bar {
    height: 36px;
    padding: 0 16px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    cursor: grab;
    z-index: 20;
    flex-shrink: 0;
  }

  .window-bar:active {
    cursor: grabbing;
  }

  .gateway-indicator {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .gw-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
  }

  .gw-dot.online {
    background: #84cc16;
    box-shadow: 0 0 8px rgba(132, 204, 22, 0.7);
  }

  .gw-dot.offline {
    background: #ef4444;
  }

  .gw-label {
    font-size: 10.5px;
    color: #52525b;
    font-family: "SF Mono", Consolas, monospace;
    letter-spacing: 0.02em;
  }

  .win-actions {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .win-btn {
    width: 24px;
    height: 24px;
    border-radius: 6px;
    border: none;
    background: transparent;
    color: #52525b;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .win-btn:hover {
    color: #d4d4d8;
    background: rgba(255, 255, 255, 0.05);
  }

  .win-btn.close:hover {
    color: #f87171;
    background: rgba(239, 68, 68, 0.1);
  }

  /* ═════════════════════════════════════════════════════════════════ */
  /* LAYOUT PRINCIPAL (INSPIRADO NO PLAN WISE + PINTEREST VIDEO)      */
  /* ═════════════════════════════════════════════════════════════════ */

  .planwise-stage {
    flex: 1;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    padding: 0 18px 16px 18px;
  }

  .hero-header {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    margin-top: 2px;
    margin-bottom: 14px;
  }

  /* ANIMAÇÃO DO PINTEREST (ESFERA ESCURA + ANEL DE LUZ DIAGONAL + ESTRELA CENTRAL) */
  .orbital-squircle {
    width: 62px;
    height: 62px;
    border-radius: 18px;
    background: #060608;
    border: 1px solid rgba(255, 255, 255, 0.06);
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;
    box-shadow:
      0 12px 28px rgba(0, 0, 0, 0.7),
      inset 0 1px 0 rgba(255, 255, 255, 0.06);
    margin-bottom: 12px;
  }

  .sphere-shadow {
    position: absolute;
    width: 36px;
    height: 36px;
    border-radius: 50%;
    background: radial-gradient(circle at 32% 32%, #14161f 0%, #050507 78%);
    box-shadow: inset -4px -4px 10px rgba(0, 0, 0, 0.9);
    z-index: 1;
  }

  .orbital-assembly {
    position: relative;
    width: 56px;
    height: 56px;
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 2;
    animation: orbital-breathe 5.5s ease-in-out infinite;
  }

  .light-streak {
    position: absolute;
    width: 64px;
    height: 2px;
    background: linear-gradient(
      90deg,
      transparent 0%,
      rgba(59, 130, 246, 0.35) 20%,
      rgba(191, 219, 254, 0.95) 50%,
      rgba(59, 130, 246, 0.35) 80%,
      transparent 100%
    );
    filter: blur(0.6px);
    box-shadow: 0 0 12px rgba(96, 165, 250, 0.85);
  }

  .orbital-ring {
    position: absolute;
    width: 42px;
    height: 11px;
    border-radius: 50%;
    border: 2px solid rgba(219, 234, 254, 0.92);
    box-shadow:
      0 0 10px rgba(96, 165, 250, 0.85),
      inset 0 0 6px rgba(96, 165, 250, 0.5);
    animation: ring-tilt 5.5s ease-in-out infinite;
  }

  .core-star {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: #ffffff;
    box-shadow:
      0 0 8px #ffffff,
      0 0 16px #60a5fa,
      0 0 26px #3b82f6;
    z-index: 3;
    animation: core-pulse 2.75s ease-in-out infinite;
  }

  @keyframes orbital-breathe {
    0%, 100% {
      transform: rotate(-52deg) scale(1);
    }
    50% {
      transform: rotate(-36deg) scale(1.04);
    }
  }

  @keyframes ring-tilt {
    0%, 100% {
      transform: scaleY(0.42);
    }
    50% {
      transform: scaleY(0.85);
    }
  }

  @keyframes core-pulse {
    0%, 100% {
      transform: scale(0.92);
      opacity: 0.9;
    }
    50% {
      transform: scale(1.18);
      opacity: 1;
    }
  }

  .main-title {
    margin: 0 0 5px 0;
    font-size: 20px;
    font-weight: 600;
    color: #fafafa;
    letter-spacing: -0.025em;
  }

  .main-subtitle {
    margin: 0;
    font-size: 12.5px;
    color: #71717a;
    line-height: 1.42;
    max-width: 310px;
  }

  /* QUADRO INTERNO BIPARTIDO (PLAN WISE) */
  .inner-board {
    background: #17171a;
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 16px;
    padding: 12px 14px;
    display: grid;
    grid-template-columns: 1.05fr 1px 1fr;
    gap: 12px;
    min-height: 224px;
  }

  .board-divider {
    background: linear-gradient(
      180deg,
      transparent 0%,
      rgba(255, 255, 255, 0.07) 20%,
      rgba(255, 255, 255, 0.07) 80%,
      transparent 100%
    );
  }

  .board-col {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .col-step-row {
    display: flex;
    justify-content: flex-end;
    margin-bottom: 2px;
  }

  .step-tag {
    font-size: 10.5px;
    color: #52525b;
    font-weight: 500;
  }

  .col-heading {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12.5px;
    font-weight: 500;
    color: #f4f4f5;
    margin-bottom: 10px;
  }

  .dots-grid {
    color: #52525b;
    font-size: 12px;
  }

  .items-list {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 5px;
    overflow-y: auto;
    max-height: 145px;
  }

  .empty-keys {
    flex: 1;
    display: flex;
    flex-direction: column;
    justify-content: center;
    color: #52525b;
    font-size: 11.5px;
    gap: 3px;
  }

  .empty-keys small {
    color: #3f3f46;
    font-size: 10.5px;
  }

  .key-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 8px;
    border-radius: 8px;
    cursor: pointer;
    border: 1px solid transparent;
    transition: all 0.15s ease;
  }

  .key-row:hover {
    background: rgba(255, 255, 255, 0.03);
  }

  .key-row.selected {
    background: #232524;
    border-color: rgba(163, 230, 53, 0.14);
  }

  .key-row-left {
    display: flex;
    align-items: center;
    gap: 7px;
    min-width: 0;
  }

  .check-lime {
    color: #84cc16;
    font-size: 11px;
    font-weight: 700;
  }

  .prov-badge {
    width: 7px;
    height: 7px;
    border-radius: 2px;
    flex-shrink: 0;
  }

  .prov-badge.openrouter {
    background: #818cf8;
  }
  .prov-badge.gemini {
    background: #38bdf8;
  }
  .prov-badge.anthropic {
    background: #fb923c;
  }

  .key-alias {
    font-size: 12px;
    color: #d4d4d8;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .key-row.selected .key-alias {
    color: #ffffff;
    font-weight: 500;
  }

  .key-row-right {
    display: flex;
    align-items: center;
    gap: 5px;
    flex-shrink: 0;
  }

  .team-pill {
    background: #84cc16;
    color: #090d04;
    font-size: 9px;
    font-weight: 800;
    padding: 2px 5px;
    border-radius: 4px;
    letter-spacing: 0.03em;
  }

  .linked-tag {
    font-size: 10px;
    color: #60a5fa;
  }

  .del-micro {
    background: none;
    border: none;
    color: #52525b;
    font-size: 13px;
    padding: 0 2px;
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.15s;
  }

  .key-row:hover .del-micro {
    opacity: 1;
  }

  .del-micro:hover {
    color: #f87171;
  }

  .col-footer-meta {
    margin-top: 8px;
    padding-top: 6px;
    display: flex;
    align-items: center;
    gap: 7px;
    font-size: 11px;
    color: #71717a;
  }

  .meta-square {
    width: 11px;
    height: 11px;
    border: 1px solid #3f3f46;
    border-radius: 3px;
  }

  .meta-text {
    font-family: "SF Mono", Consolas, monospace;
    font-size: 10.5px;
  }

  /* COLUNA DIREITA: AÇÕES DE ACESSO RÁPIDO */
  .access-list {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }

  .access-row {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    border-radius: 8px;
    background: transparent;
    border: 1px solid transparent;
    color: #a1a1aa;
    font-size: 11.5px;
    cursor: pointer;
    text-align: left;
    transition: all 0.15s ease;
  }

  .access-row:hover:not(:disabled) {
    background: #222226;
    color: #f4f4f5;
  }

  .access-row.active-row {
    background: #232524;
    border-color: rgba(132, 204, 22, 0.2);
    color: #ffffff;
  }

  .access-row:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .avatar-badge {
    width: 20px;
    height: 20px;
    border-radius: 6px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 10px;
    font-weight: 700;
    flex-shrink: 0;
  }

  .avatar-badge.purple {
    background: rgba(168, 85, 247, 0.18);
    color: #c084fc;
  }
  .avatar-badge.blue {
    background: rgba(56, 189, 248, 0.16);
    color: #38bdf8;
  }
  .avatar-badge.amber {
    background: rgba(251, 146, 60, 0.16);
    color: #fb923c;
  }
  .avatar-badge.dark {
    background: rgba(255, 255, 255, 0.07);
    color: #d4d4d8;
  }

  .access-label {
    flex: 1;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .access-check {
    font-size: 10px;
    color: #84cc16;
    font-weight: 600;
  }

  /* FAIXA DE 3 PILLS DE CONFIGURAÇÃO (SCHEDULE / ACTIVE / PIN) */
  .Quick-pills-row {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
    margin: 10px 0;
  }

  .config-pill {
    background: #161618;
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 10px;
    padding: 8px 10px;
    display: flex;
    align-items: center;
    gap: 5px;
    color: #a1a1aa;
    font-size: 11px;
    cursor: pointer;
    transition: all 0.15s ease;
    white-space: nowrap;
    overflow: hidden;
  }

  .config-pill:hover {
    border-color: rgba(255, 255, 255, 0.13);
    color: #f4f4f5;
    background: #1b1b1f;
  }

  .config-pill small {
    color: #52525b;
    font-size: 10.5px;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .pill-arrow {
    margin-left: auto;
    color: #52525b;
  }

  /* RODAPÉ INTEGRADO (TOOLBAR + DOCK PLAN WISE) */
  .bottom-dock-container {
    background: #18181b;
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 16px;
    overflow: hidden;
  }

  .dock-toolbar {
    padding: 8px 14px;
    background: #202024;
    border-bottom: 1px solid rgba(255, 255, 255, 0.04);
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 11px;
    color: #71717a;
  }

  .tool-item {
    background: none;
    border: none;
    color: #71717a;
    font-size: 11px;
    padding: 0;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 4px;
    transition: color 0.15s;
  }

  .tool-item:hover {
    color: #e4e4e7;
  }

  .tool-sep {
    color: #3f3f46;
  }

  .notice-inline {
    margin-left: auto;
    color: #a3e635;
    font-size: 10.5px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 210px;
  }

  .dock-input-bar {
    padding: 10px 14px;
    background: #121214;
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .lime-orb {
    width: 22px;
    height: 22px;
    border-radius: 50%;
    flex-shrink: 0;
    background: radial-gradient(
      circle at 32% 30%,
      #d9f99d 0%,
      #84cc16 48%,
      #15803d 85%,
      #052e16 100%
    );
    box-shadow: 0 0 14px rgba(132, 204, 22, 0.45);
  }

  .cmd-prefix {
    font-size: 12.5px;
    color: #52525b;
    font-weight: 500;
  }

  .cmd-pipe {
    color: #3f3f46;
    font-size: 13px;
  }

  .input-alias {
    width: 110px;
    background: transparent;
    border: none;
    color: #f4f4f5;
    font-size: 12.5px;
    outline: none;
  }

  .input-secret {
    flex: 1;
    background: transparent;
    border: none;
    border-left: 1px solid #27272a;
    padding-left: 8px;
    color: #f4f4f5;
    font-size: 12.5px;
    outline: none;
  }

  .input-alias::placeholder,
  .input-secret::placeholder {
    color: #52525b;
  }

  .dock-send-btn {
    background: none;
    border: none;
    color: #71717a;
    cursor: pointer;
    padding: 4px;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: color 0.15s, transform 0.15s;
  }

  .dock-send-btn:hover:not(:disabled) {
    color: #a3e635;
    transform: translateX(1px);
  }

  .dock-send-btn:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  /* ═════════════════════════════════════════════════════════════════ */
  /* TELA DE BLOQUEIO 6 DÍGITOS (REFERÊNCIA 3 - CADEADO METÁLICO)     */
  /* ═════════════════════════════════════════════════════════════════ */

  .lock-screen {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 0 28px 24px 28px;
    outline: none;
    background:
      repeating-linear-gradient(
        0deg,
        rgba(255, 255, 255, 0.012) 0px,
        rgba(255, 255, 255, 0.012) 1px,
        transparent 1px,
        transparent 3px
      ),
      radial-gradient(circle at 65% 32%, rgba(30, 58, 138, 0.22) 0%, transparent 55%),
      #080b12;
  }

  .padlock-stage {
    position: relative;
    width: 140px;
    height: 170px;
    display: flex;
    justify-content: center;
    margin-bottom: 10px;
  }

  .ambient-blue-glow {
    position: absolute;
    width: 160px;
    height: 160px;
    background: radial-gradient(circle, rgba(59, 130, 246, 0.18) 0%, transparent 70%);
    top: 10px;
  }

  .padlock-3d {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    z-index: 2;
  }

  .padlock-shackle {
    width: 62px;
    height: 68px;
    border: 13px solid #1e2433;
    border-bottom: none;
    border-radius: 40px 40px 0 0;
    box-shadow:
      inset 0 2px 3px rgba(255, 255, 255, 0.28),
      inset 3px 0 4px rgba(147, 197, 253, 0.18),
      0 6px 16px rgba(0, 0, 0, 0.8);
  }

  .padlock-body {
    width: 112px;
    height: 92px;
    border-radius: 8px;
    background: linear-gradient(135deg, #252b3b 0%, #10141f 55%, #07090f 100%);
    border-top: 1px solid rgba(255, 255, 255, 0.15);
    display: flex;
    align-items: center;
    justify-content: center;
    box-shadow: 0 14px 30px rgba(0, 0, 0, 0.9);
  }

  .padlock-emblem {
    opacity: 0.7;
  }

  .padlock-fade {
    position: absolute;
    bottom: -4px;
    left: -10px;
    right: -10px;
    height: 44px;
    background: linear-gradient(180deg, transparent 0%, #080b12 92%);
  }

  .lock-title {
    margin: 0 0 6px 0;
    font-size: 20px;
    font-weight: 700;
    color: #ffffff;
    letter-spacing: -0.02em;
  }

  .lock-subtitle {
    margin: 0 0 22px 0;
    font-size: 11.5px;
    color: #64748b;
    text-align: center;
    line-height: 1.45;
    max-width: 260px;
  }

  .otp-row {
    display: flex;
    gap: 8px;
    margin-bottom: 20px;
  }

  .otp-slot {
    width: 44px;
    height: 50px;
    border-radius: 11px;
    background: rgba(15, 23, 42, 0.75);
    border: 1px solid rgba(255, 255, 255, 0.07);
    display: flex;
    align-items: center;
    justify-content: center;
    position: relative;
    cursor: pointer;
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.05);
    transition: all 0.15s ease;
  }

  .otp-slot.active {
    border-color: rgba(147, 197, 253, 0.75);
    box-shadow:
      0 0 14px rgba(59, 130, 246, 0.25),
      inset 0 0 8px rgba(59, 130, 246, 0.15);
  }

  .otp-char {
    font-size: 16px;
    font-weight: 700;
    color: #f8fafc;
  }

  .otp-caret {
    width: 1.5px;
    height: 18px;
    background: #3b82f6;
    margin-left: 1px;
    animation: caret-blink 1s step-end infinite;
  }

  @keyframes caret-blink {
    50% {
      opacity: 0;
    }
  }

  .lock-helper {
    font-size: 11px;
    color: #94a3b8;
    margin: 0;
  }

  .lock-error {
    font-size: 11.5px;
    color: #f87171;
    margin: 0;
  }

  .inline-link {
    background: none;
    border: none;
    color: #60a5fa;
    font-size: 11px;
    cursor: pointer;
    padding: 0;
    text-decoration: underline;
  }

  /* ═════════════════════════════════════════════════════════════════ */
  /* CARD DE TOKEN VIRTUAL (REFERÊNCIA 1 - DAYBRIDGE)                 */
  /* ═════════════════════════════════════════════════════════════════ */

  .daybridge-stage {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    position: relative;
    padding: 20px;
  }

  .purple-orbit-lines {
    position: absolute;
    width: 420px;
    height: 420px;
    border-radius: 50%;
    border: 1.5px solid rgba(139, 92, 246, 0.28);
    box-shadow: inset 0 0 40px rgba(139, 92, 246, 0.08);
    pointer-events: none;
  }

  .daybridge-outer {
    width: 340px;
    background: #17161d;
    border: 1px solid rgba(255, 255, 255, 0.07);
    border-radius: 24px;
    padding: 14px;
    z-index: 2;
    box-shadow: 0 24px 60px rgba(0, 0, 0, 0.85);
  }

  .daybridge-inner {
    background: radial-gradient(
      circle at 50% 35%,
      rgba(139, 92, 246, 0.18) 0%,
      rgba(30, 27, 46, 0.65) 55%,
      #1b1924 100%
    );
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 18px;
    padding: 20px 18px;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    min-height: 260px;
    text-align: center;
  }

  .db-top-meta {
    display: flex;
    justify-content: space-between;
    font-family: "SF Mono", Consolas, monospace;
    font-size: 10px;
    color: #e4e4e7;
    letter-spacing: 0.05em;
  }

  .db-top-meta small {
    color: #71717a;
    margin-right: 3px;
  }

  .db-center-block {
    margin: 28px 0;
  }

  .db-sparkle-icon {
    color: #c084fc;
    font-size: 14px;
    margin-bottom: 6px;
  }

  .db-invite-label {
    font-family: "SF Mono", Consolas, monospace;
    font-size: 10.5px;
    letter-spacing: 0.12em;
    color: #a855f7;
    margin-bottom: 10px;
  }

  .db-invite-code {
    font-family: "SF Mono", Consolas, monospace;
    font-size: 22px;
    font-weight: 600;
    letter-spacing: 0.08em;
    color: #ffffff;
  }

  .db-bottom-meta {
    font-family: "SF Mono", Consolas, monospace;
    font-size: 10.5px;
    color: #71717a;
    letter-spacing: 0.06em;
  }

  .db-bottom-meta strong {
    color: #f4f4f5;
    margin-left: 4px;
  }

  .daybridge-footer {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 14px 8px 4px 8px;
  }

  .db-copy-action {
    background: rgba(168, 85, 247, 0.16);
    border: 1px solid rgba(168, 85, 247, 0.35);
    color: #e9d5ff;
    font-size: 11.5px;
    font-weight: 500;
    padding: 6px 12px;
    border-radius: 8px;
    cursor: pointer;
  }

  .db-back-btn {
    background: none;
    border: none;
    color: #71717a;
    font-size: 11.5px;
    cursor: pointer;
  }

  .db-back-btn:hover {
    color: #d4d4d8;
  }

  /* ESTILOS DA ASSISTENTE SEXTA-FEIRA (F.R.I.D.A.Y.) */
  .friday-pill-active {
    border-color: rgba(163, 230, 53, 0.45) !important;
    background: rgba(163, 230, 53, 0.09) !important;
    color: #ecfccb !important;
  }

  .friday-pill-active small {
    color: #a3e635 !important;
    font-weight: 600;
  }

  .friday-pill-locked {
    opacity: 0.42;
    cursor: not-allowed !important;
  }

  .orbital-squircle.friday-online {
    box-shadow:
      0 14px 28px rgba(0, 0, 0, 0.75),
      0 0 18px rgba(163, 230, 53, 0.22),
      inset 0 1px 1px rgba(255, 255, 255, 0.14);
  }

  .orbital-squircle.friday-speaking {
    animation: fridayPulse 0.9s ease-in-out infinite alternate;
  }

  @keyframes fridayPulse {
    0% {
      transform: scale(1);
      box-shadow:
        0 14px 28px rgba(0, 0, 0, 0.75),
        0 0 16px rgba(56, 189, 248, 0.35),
        inset 0 1px 1px rgba(255, 255, 255, 0.2);
    }
    100% {
      transform: scale(1.06);
      box-shadow:
        0 14px 32px rgba(0, 0, 0, 0.8),
        0 0 28px rgba(163, 230, 53, 0.55),
        inset 0 1px 2px rgba(255, 255, 255, 0.35);
    }
  }

  .tool-item.active-tool {
    color: #e4e4e7;
    font-weight: 500;
  }

  .speaking-orb {
    animation: orbTalk 0.6s ease-in-out infinite alternate;
  }

  @keyframes orbTalk {
    from {
      transform: scale(0.95);
      filter: brightness(1);
    }
    to {
      transform: scale(1.18);
      filter: brightness(1.35);
    }
  }
</style>
