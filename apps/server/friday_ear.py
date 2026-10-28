import av
import base64
import io
import json
import queue
import time
import unicodedata
import urllib.request
import numpy as np
import sounddevice as sd
from faster_whisper import WhisperModel

GATEWAY = "http://127.0.0.1:8090"
SAMPLE_RATE = 16000
BLOCK_SIZE = 3200  # 200ms por bloco

def strip_accents(s: str) -> str:
    return "".join(
        c for c in unicodedata.normalize("NFD", s.lower())
        if unicodedata.category(c) != "Mn"
    )

def play_mp3_sounddevice(b64_mp3: str):
    if not b64_mp3:
        return
    try:
        raw = base64.b64decode(b64_mp3)
        container = av.open(io.BytesIO(raw))
        frames = [f.to_ndarray() for f in container.decode(audio=0)]
        if not frames:
            return
        rate = container.streams.audio[0].rate
        arr = np.concatenate(frames, axis=-1).T.astype("float32")
        sd.play(arr, rate)
        sd.wait()
    except Exception as e:
        print("[FRIDAY-EAR] Erro ao tocar MP3:", e, flush=True)

def load_cli_token() -> str:
    home = os.environ.get("KEYHYBRID_HOME") or os.environ.get("HOME") or os.environ.get("USERPROFILE") or "."
    path = os.path.join(home, ".keyhybrid", ".cli.token")
    try:
        with open(path, "r", encoding="utf-8") as f:
            return f.read().strip()
    except Exception:
        return ""

def check_friday_enabled() -> bool:
    try:
        req = urllib.request.Request(f"{GATEWAY}/api/keyhybrid/friday/state", method="GET")
        tok = load_cli_token()
        if tok:
            req.add_header("X-KeyHybrid-Token", tok)
        with urllib.request.urlopen(req, timeout=1.0) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            return bool(data.get("enabled", True))
    except Exception:
        return True

def is_friday_command(text: str) -> bool:
    norm = strip_accents(text).strip().lower()
    if len(norm) < 3:
        return False
    wake_words = ("sexta", "sista", "cesta", "sesta", "friday")
    words = norm.split()
    if not words:
        return False
    return any(words[0].startswith(w) for w in wake_words)

def send_to_friday(text: str):
    try:
        t0 = time.time()
        payload = json.dumps({"text": text}).encode("utf-8")
        req = urllib.request.Request(
            f"{GATEWAY}/api/keyhybrid/friday/assist",
            data=payload,
            headers={
                "Content-Type": "application/json; charset=utf-8",
                "X-KeyHybrid-Token": load_cli_token(),
            },
            method="POST"
        )
        with urllib.request.urlopen(req, timeout=30) as resp:
            res = json.loads(resp.read().decode("utf-8"))
            dt = round(time.time() - t0, 2)
            print(f"[FRIDAY-EAR] ({dt}s) Resposta: {res.get('reply')} | Light: {res.get('light_status')}", flush=True)
            audio_b64 = res.get("audio_base64", "")
            if audio_b64:
                play_mp3_sounddevice(audio_b64)
    except Exception as e:
        print(f"[FRIDAY-EAR] Erro ao enviar comando: {e}", flush=True)

def main():
    print("[FRIDAY-EAR] Carregando faster-whisper (base)...", flush=True)
    model = WhisperModel("base", device="cpu", compute_type="int8")
    print("[FRIDAY-EAR] Microfone pronto e calibrado! Escutando comandos...", flush=True)

    q = queue.Queue()

    def audio_callback(indata, frames, time_info, status):
        q.put(indata[:, 0].copy())

    with sd.InputStream(samplerate=SAMPLE_RATE, channels=1, dtype="float32", blocksize=BLOCK_SIZE, callback=audio_callback):
        speech_chunks = []
        silence_blocks = 0
        speaking = False
        peak_rms = 0.0

        while True:
            chunk = q.get()
            rms = float(np.sqrt(np.mean(chunk ** 2)))

            if not speaking:
                # Calibrado para o ganho real do seu microfone (ruído de fundo ~0.0038, voz ~0.0075+)
                if rms > 0.0072:
                    if not check_friday_enabled():
                        continue
                    speaking = True
                    peak_rms = rms
                    speech_chunks = [chunk]
                    silence_blocks = 0
            else:
                speech_chunks.append(chunk)
                if rms > peak_rms:
                    peak_rms = rms
                stop_thresh = max(0.0058, peak_rms * 0.42)
                if rms < stop_thresh:
                    silence_blocks += 1
                else:
                    silence_blocks = 0

                # Fecha a frase após 0.4s de silêncio (2 blocos) ou máx 2.4s (12 blocos)
                if silence_blocks >= 2 or len(speech_chunks) >= 12:
                    audio_np = np.concatenate(speech_chunks)
                    speaking = False
                    speech_chunks = []
                    silence_blocks = 0

                    if len(audio_np) < SAMPLE_RATE * 0.35:
                        continue

                    try:
                        segments, _ = model.transcribe(
                            audio_np,
                            language="pt",
                            beam_size=1,
                            vad_filter=True,
                            vad_parameters=dict(min_silence_duration_ms=200),
                            condition_on_previous_text=False
                        )
                        transcript = " ".join(s.text.strip() for s in segments).strip()
                        if transcript:
                            print(f"[FRIDAY-EAR] Ouvido (peak={peak_rms:.4f}): '{transcript}'", flush=True)
                            if is_friday_command(transcript):
                                send_to_friday(transcript)
                                time.sleep(0.15)
                                while not q.empty():
                                    try:
                                        q.get_nowait()
                                    except Exception:
                                        break
                    except Exception as e:
                        print("[FRIDAY-EAR] Erro de transcrição:", e, flush=True)

if __name__ == "__main__":
    main()
