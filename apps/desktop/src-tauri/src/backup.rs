// Backup e restauração portátil do cofre (#26). O servidor Go despeja/aplica o cofre em claro
// por pipe (vault-dump / vault-apply); aqui o documento é selado no arquivo .khvault.
//
// Formato: MAGIC(8) | versão u8 | flags u8 | salt[16] | nonce[12] | ciphertext||tag(16)
// AAD = os 38 bytes do cabeçalho. Chave = Argon2id v0x13 (m=65536, t=3, p=4) sobre a senha.
use crate::vault::{assert_local_origin, has_vault_pin, is_unlocked};
use aes_gcm::aead::{Aead, KeyInit, Payload};
use aes_gcm::{Aes256Gcm, Nonce};
use argon2::{Algorithm, Argon2, Params, Version};
use serde::{Deserialize, Serialize};
use std::fs;
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use tauri_plugin_dialog::DialogExt;
use zeroize::{Zeroize, Zeroizing};

const MAGIC: &[u8; 8] = b"KHVAULT\x00";
const FILE_VERSION: u8 = 1;
const HEADER_LEN: usize = 38;
const TAG_LEN: usize = 16;
const MAX_FILE: u64 = 16 << 20;
const MIN_PASSPHRASE: usize = 12;

const ERR_CORROMPIDO: &str = "Arquivo corrompido ou não é um backup do KeyHybrid.";
const ERR_VERSAO_NOVA: &str = "Backup de versão mais nova: atualize o KeyHybrid para restaurar.";
const ERR_SENHA: &str = "Senha incorreta ou arquivo alterado.";
const ERR_SENHA_CURTA: &str = "A senha do backup precisa de pelo menos 12 caracteres.";
const ERR_BLOQUEADO: &str =
    "Cofre bloqueado. Digite seu PIN de 6 dígitos para autorizar o backup ou a restauração.";
const ERR_SERVIDOR_ANTIGO: &str =
    "Atualize o KeyHybrid: o servidor instalado não tem o comando de backup.";

#[derive(Serialize)]
pub struct BackupResult {
    path: String,
    items: usize,
    created_at: String,
}

#[derive(Serialize)]
pub struct BackupPreview {
    created_at: String,
    app_version: String,
    total: usize,
    ai_keys: usize,
    service_keys: usize,
    providers: Vec<String>,
    aliases: Vec<String>,
    conflicts: Vec<String>,
}

#[derive(Serialize)]
pub struct RestoreResult {
    mode: String,
    added: Vec<String>,
    conflicts: Vec<String>,
    removed: usize,
    total_after: usize,
    safety_backup: Option<String>,
}

#[derive(Serialize, Deserialize, Default)]
pub struct BackupStatus {
    last_backup: Option<StatusBackup>,
    last_restore: Option<StatusRestore>,
}

#[derive(Serialize, Deserialize)]
struct StatusBackup {
    at: String,
    path: String,
    items: usize,
}

#[derive(Serialize, Deserialize)]
struct StatusRestore {
    at: String,
    path: String,
    items: usize,
    mode: String,
}

/// Documento do cofre com os segredos em claro: zera todas as strings ao sair de escopo
#[derive(Serialize, Deserialize, PartialEq, Debug)]
#[serde(transparent)]
struct PlainVault(serde_json::Value);

impl Drop for PlainVault {
    fn drop(&mut self) {
        wipe(&mut self.0);
    }
}

// Só os valores: as chaves dos objetos são nomes de campo, nunca segredo
fn wipe(v: &mut serde_json::Value) {
    match v {
        serde_json::Value::String(s) => s.zeroize(),
        serde_json::Value::Array(a) => a.iter_mut().for_each(wipe),
        serde_json::Value::Object(m) => m.values_mut().for_each(wipe),
        _ => {}
    }
}

// Capacidade fixa: o JSON não realoca, e cada realocação deixaria uma cópia em claro no heap
fn to_json(v: &impl Serialize) -> Result<Zeroizing<Vec<u8>>, String> {
    let mut buf = Zeroizing::new(Vec::with_capacity(MAX_FILE as usize));
    serde_json::to_writer(&mut *buf, v).map_err(|e| e.to_string())?;
    Ok(buf)
}

// Só os campos que a prévia usa; o segredo segue dentro do PlainVault até o vault-apply
#[derive(Deserialize)]
struct VaultDoc {
    entries: Vec<VaultEntry>,
}

#[derive(Deserialize)]
struct VaultEntry {
    alias: String,
    provider: String,
    kind: String,
}

#[derive(Deserialize)]
struct ApplyReport {
    mode: String,
    added: Vec<String>,
    conflicts: Vec<String>,
    removed: usize,
    total_after: usize,
}

#[derive(Serialize, Deserialize)]
struct Envelope {
    format: String,
    version: u32,
    created_at: String,
    app_version: String,
    vault: PlainVault,
}

/// Mesma regra do homeDir() do servidor Go (apps/server/guard.go)
fn khhome() -> PathBuf {
    ["KEYHYBRID_HOME", "HOME", "USERPROFILE"]
        .iter()
        .find_map(|v| std::env::var(v).ok().filter(|s| !s.is_empty()))
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("."))
}

// Data civil UTC a partir do relógio (algoritmo de Howard Hinnant), sem dependência de calendário
fn utc_now() -> (i64, u32, u32, u32, u32, u32) {
    let secs = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs() as i64;
    utc_parts(secs)
}

fn utc_parts(secs: i64) -> (i64, u32, u32, u32, u32, u32) {
    let (days, rem) = (secs.div_euclid(86_400), secs.rem_euclid(86_400));
    let z = days + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z - era * 146_097;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = (doy - (153 * mp + 2) / 5 + 1) as u32;
    let m = if mp < 10 { mp + 3 } else { mp - 9 } as u32;
    let y = yoe + era * 400 + i64::from(m <= 2);
    (
        y,
        m,
        d,
        (rem / 3600) as u32,
        (rem % 3600 / 60) as u32,
        (rem % 60) as u32,
    )
}

fn rfc3339((y, m, d, hh, mm, ss): (i64, u32, u32, u32, u32, u32)) -> String {
    format!("{y:04}-{m:02}-{d:02}T{hh:02}:{mm:02}:{ss:02}Z")
}

fn compact((y, m, d, hh, mm, ss): (i64, u32, u32, u32, u32, u32)) -> String {
    format!("{y:04}{m:02}{d:02}-{hh:02}{mm:02}{ss:02}")
}

fn derive_key(passphrase: &str, salt: &[u8]) -> Result<Zeroizing<[u8; 32]>, String> {
    let params = Params::new(65536, 3, 4, Some(32)).map_err(|e| e.to_string())?;
    let mut key = Zeroizing::new([0u8; 32]);
    Argon2::new(Algorithm::Argon2id, Version::V0x13, params)
        .hash_password_into(passphrase.as_bytes(), salt, key.as_mut())
        .map_err(|e| format!("Falha ao derivar a chave do backup: {e}"))?;
    Ok(key)
}

fn seal(plaintext: &[u8], passphrase: &str) -> Result<Vec<u8>, String> {
    if passphrase.chars().count() < MIN_PASSPHRASE {
        return Err(ERR_SENHA_CURTA.to_string());
    }
    let mut header = [0u8; HEADER_LEN];
    header[..8].copy_from_slice(MAGIC);
    header[8] = FILE_VERSION;
    header[9] = 0;
    getrandom::fill(&mut header[10..HEADER_LEN]).map_err(|e| e.to_string())?;
    let key = derive_key(passphrase, &header[10..26])?;
    let cipher = Aes256Gcm::new_from_slice(key.as_ref()).map_err(|e| e.to_string())?;
    let ct = cipher
        .encrypt(
            Nonce::from_slice(&header[26..HEADER_LEN]),
            Payload {
                msg: plaintext,
                aad: &header,
            },
        )
        .map_err(|_| "Falha ao cifrar o backup.".to_string())?;
    let mut out = header.to_vec();
    out.extend_from_slice(&ct);
    Ok(out)
}

fn open(data: &[u8], passphrase: &str) -> Result<Zeroizing<Vec<u8>>, String> {
    if data.len() < HEADER_LEN + TAG_LEN || &data[..8] != MAGIC {
        return Err(ERR_CORROMPIDO.to_string());
    }
    if data[8] > FILE_VERSION {
        return Err(ERR_VERSAO_NOVA.to_string());
    }
    if data[8] == 0 || data[9] != 0 {
        return Err(ERR_CORROMPIDO.to_string());
    }
    let key = derive_key(passphrase, &data[10..26])?;
    let cipher = Aes256Gcm::new_from_slice(key.as_ref()).map_err(|e| e.to_string())?;
    cipher
        .decrypt(
            Nonce::from_slice(&data[26..HEADER_LEN]),
            Payload {
                msg: &data[HEADER_LEN..],
                aad: &data[..HEADER_LEN],
            },
        )
        .map(Zeroizing::new)
        .map_err(|_| ERR_SENHA.to_string())
}

/// Roda o servidor Go com o mesmo KEYHYBRID_HOME do app e devolve o stdout
fn run_server(
    bin: &Path,
    home: &Path,
    args: &[&str],
    stdin: Option<&[u8]>,
) -> Result<Zeroizing<Vec<u8>>, String> {
    let mut cmd = Command::new(bin);
    cmd.args(args)
        .env("KEYHYBRID_HOME", home)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    #[cfg(target_os = "windows")]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(0x08000000); // CREATE_NO_WINDOW
    }
    let mut child = cmd
        .spawn()
        .map_err(|e| format!("Não foi possível iniciar o servidor do KeyHybrid: {e}"))?;
    let mut pipe = child.stdin.take();
    if let (Some(p), Some(bytes)) = (pipe.as_mut(), stdin) {
        // O filho pode recusar a entrada e fechar o pipe antes; o código de saída explica
        let _ = p.write_all(bytes);
    }
    drop(pipe);
    // stderr em outra thread para o filho não travar com o pipe cheio
    let mut err_pipe = child.stderr.take();
    let err_thread = std::thread::spawn(move || {
        let mut buf = Vec::new();
        if let Some(p) = err_pipe.as_mut() {
            let _ = p.read_to_end(&mut buf);
        }
        buf
    });
    // stdout lido num buffer já do tamanho máximo: sem realocação, nenhuma cópia do dump fica solta.
    // Passou de 16 MiB: o pipe fecha, o filho recebe EPIPE e a saída inválida vira erro abaixo.
    let mut stdout = Zeroizing::new(Vec::with_capacity(MAX_FILE as usize + 1));
    let lido = child
        .stdout
        .take()
        .map(|p| p.take(MAX_FILE).read_to_end(&mut stdout));
    let status = child
        .wait()
        .map_err(|e| format!("Falha ao aguardar o servidor do KeyHybrid: {e}"))?;
    let stderr_bytes = err_thread.join().unwrap_or_default();
    if let Some(Err(e)) = lido {
        return Err(format!(
            "Falha ao ler a resposta do servidor do KeyHybrid: {e}"
        ));
    }
    if status.success() {
        return Ok(stdout);
    }
    let stderr = String::from_utf8_lossy(&stderr_bytes);
    if stderr.contains("unknown command") {
        return Err(ERR_SERVIDOR_ANTIGO.to_string());
    }
    // O servidor nunca escreve segredo no stderr; a última linha é a do motivo
    let motivo = stderr
        .lines()
        .rev()
        .find(|l| !l.trim().is_empty())
        .unwrap_or("sem mensagem");
    Err(format!(
        "O servidor recusou a operação ({}): {motivo}",
        status.code().map_or("sinal".to_string(), |c| c.to_string())
    ))
}

/// Costura do subprocesso: o app usa o binário Go; os testes usam um cofre em memória
trait Runner {
    fn run(
        &self,
        home: &Path,
        args: &[&str],
        stdin: Option<&[u8]>,
    ) -> Result<Zeroizing<Vec<u8>>, String>;
}

impl Runner for PathBuf {
    fn run(
        &self,
        home: &Path,
        args: &[&str],
        stdin: Option<&[u8]>,
    ) -> Result<Zeroizing<Vec<u8>>, String> {
        run_server(self, home, args, stdin)
    }
}

fn server_bin() -> Result<PathBuf, String> {
    crate::find_server_bin()
        .ok_or_else(|| "Servidor do KeyHybrid não encontrado ao lado do app.".to_string())
}

fn dump_vault(bin: &dyn Runner, home: &Path) -> Result<PlainVault, String> {
    let out = bin.run(home, &["vault-dump"], None)?;
    serde_json::from_slice(&out).map_err(|_| "O servidor devolveu um cofre ilegível.".to_string())
}

fn apply_vault(
    bin: &dyn Runner,
    home: &Path,
    vault: &PlainVault,
    mode: &str,
    dry_run: bool,
) -> Result<ApplyReport, String> {
    let input = to_json(vault)?;
    let mut args = vec!["vault-apply", "--mode", mode];
    if dry_run {
        args.push("--dry-run");
    }
    let out = bin.run(home, &args, Some(&input))?;
    serde_json::from_slice(&out)
        .map_err(|_| "O servidor devolveu um relatório ilegível.".to_string())
}

fn count_entries(vault: &PlainVault) -> Result<VaultDoc, String> {
    VaultDoc::deserialize(&vault.0).map_err(|_| ERR_CORROMPIDO.to_string())
}

fn seal_vault(vault: PlainVault, passphrase: &str, created_at: &str) -> Result<Vec<u8>, String> {
    let env = Envelope {
        format: "keyhybrid-backup".to_string(),
        version: 1,
        created_at: created_at.to_string(),
        app_version: env!("CARGO_PKG_VERSION").to_string(),
        vault,
    };
    let plain = to_json(&env)?;
    seal(&plain, passphrase)
}

fn read_backup(path: &Path, passphrase: &str) -> Result<Envelope, String> {
    let meta = fs::metadata(path).map_err(|e| format!("Não foi possível abrir o backup: {e}"))?;
    if meta.len() > MAX_FILE {
        return Err("Arquivo maior que 16 MiB: não é um backup do KeyHybrid.".to_string());
    }
    let data = fs::read(path).map_err(|e| format!("Não foi possível ler o backup: {e}"))?;
    let plain = open(&data, passphrase)?;
    let env: Envelope = serde_json::from_slice(&plain).map_err(|_| ERR_CORROMPIDO.to_string())?;
    if env.format != "keyhybrid-backup" || env.version != 1 {
        return Err(ERR_CORROMPIDO.to_string());
    }
    Ok(env)
}

fn status_path(home: &Path) -> PathBuf {
    home.join(".keyhybrid").join("backup-status.json")
}

fn read_status(home: &Path) -> Result<BackupStatus, String> {
    match fs::read_to_string(status_path(home)) {
        Ok(s) => serde_json::from_str(&s)
            .map_err(|_| "Histórico de backup ilegível (backup-status.json).".to_string()),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(BackupStatus::default()),
        Err(e) => Err(format!("Não foi possível ler o histórico de backup: {e}")),
    }
}

// A operação principal já deu certo; falha aqui só some do histórico, então avisa no log
fn record_status(home: &Path, update: impl FnOnce(&mut BackupStatus)) {
    let res = read_status(home).and_then(|mut st| {
        update(&mut st);
        let path = status_path(home);
        if let Some(dir) = path.parent() {
            fs::create_dir_all(dir).map_err(|e| e.to_string())?;
        }
        let json = serde_json::to_string_pretty(&st).map_err(|e| e.to_string())?;
        fs::write(&path, json).map_err(|e| e.to_string())
    });
    if let Err(e) = res {
        eprintln!("[KeyHybrid Backup] Não foi possível gravar o histórico de backup: {e}");
    }
}

// Grava em "<destino>.part" e renomeia: uma falha no meio nunca destrói o backup que já estava lá.
// Só ciphertext passa pelo disco.
fn write_atomic(path: &Path, bytes: &[u8]) -> std::io::Result<()> {
    use std::io::Write;
    let mut part = path.as_os_str().to_owned();
    part.push(".part");
    let part = PathBuf::from(part);
    let res = (|| {
        let mut opts = fs::OpenOptions::new();
        opts.write(true).create(true).truncate(true);
        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt;
            opts.mode(0o600);
        }
        let mut f = opts.open(&part)?;
        f.write_all(bytes)?;
        f.sync_all()?;
        fs::rename(&part, path)
    })();
    if res.is_err() {
        let _ = fs::remove_file(&part);
    }
    res
}

fn do_backup(
    bin: &dyn Runner,
    home: &Path,
    path: &Path,
    passphrase: &str,
) -> Result<BackupResult, String> {
    if passphrase.chars().count() < MIN_PASSPHRASE {
        return Err(ERR_SENHA_CURTA.to_string());
    }
    let vault = dump_vault(bin, home)?;
    let items = count_entries(&vault)?.entries.len();
    let created_at = rfc3339(utc_now());
    let sealed = seal_vault(vault, passphrase, &created_at)?;
    write_atomic(path, &sealed).map_err(|e| format!("Não foi possível salvar o backup: {e}"))?;
    let path = path.to_string_lossy().to_string();
    record_status(home, |st| {
        st.last_backup = Some(StatusBackup {
            at: created_at.clone(),
            path: path.clone(),
            items,
        })
    });
    Ok(BackupResult {
        path,
        items,
        created_at,
    })
}

fn do_inspect(
    bin: &dyn Runner,
    home: &Path,
    path: &Path,
    passphrase: &str,
) -> Result<BackupPreview, String> {
    let env = read_backup(path, passphrase)?;
    let doc = count_entries(&env.vault)?;
    let report = apply_vault(bin, home, &env.vault, "merge", true)?;
    let mut providers: Vec<String> = doc.entries.iter().map(|e| e.provider.clone()).collect();
    providers.sort();
    providers.dedup();
    Ok(BackupPreview {
        created_at: env.created_at,
        app_version: env.app_version,
        total: doc.entries.len(),
        ai_keys: doc.entries.iter().filter(|e| e.kind != "service").count(),
        service_keys: doc.entries.iter().filter(|e| e.kind == "service").count(),
        providers,
        aliases: doc.entries.into_iter().map(|e| e.alias).collect(),
        conflicts: report.conflicts,
    })
}

// Cópia de segurança do cofre atual antes de substituir tudo; falhou → nada é tocado
fn safety_backup(bin: &dyn Runner, home: &Path, passphrase: &str) -> Result<PathBuf, String> {
    let dir = home.join(".keyhybrid").join("backups");
    let mut builder = fs::DirBuilder::new();
    builder.recursive(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::DirBuilderExt;
        builder.mode(0o700);
    }
    builder
        .create(&dir)
        .map_err(|e| format!("Não foi possível criar a pasta de backups: {e}"))?;
    let now = utc_now();
    // Duas restaurações no mesmo segundo não podem apagar a cópia anterior
    let mut path = dir.join(format!(
        "keyhybrid-antes-de-restaurar-{}.khvault",
        compact(now)
    ));
    let mut n = 1;
    while path.exists() {
        path = dir.join(format!(
            "keyhybrid-antes-de-restaurar-{}-{n}.khvault",
            compact(now)
        ));
        n += 1;
    }
    let sealed = seal_vault(dump_vault(bin, home)?, passphrase, &rfc3339(now))?;
    write_atomic(&path, &sealed)
        .map_err(|e| format!("Não foi possível salvar a cópia de segurança: {e}"))?;
    Ok(path)
}

fn do_restore(
    bin: &dyn Runner,
    home: &Path,
    path: &Path,
    passphrase: &str,
    mode: &str,
    confirmation: Option<&str>,
) -> Result<RestoreResult, String> {
    if mode != "merge" && mode != "replace" {
        return Err("Modo de restauração inválido: use merge ou replace.".to_string());
    }
    if mode == "replace" && confirmation.map(str::trim) != Some("SUBSTITUIR") {
        return Err("Para substituir tudo, digite SUBSTITUIR.".to_string());
    }
    let env = read_backup(path, passphrase)?;
    let items = count_entries(&env.vault)?.entries.len();
    let safety = if mode == "replace" {
        Some(safety_backup(bin, home, passphrase)?)
    } else {
        None
    };
    let rep = apply_vault(bin, home, &env.vault, mode, false)?;
    let path = path.to_string_lossy().to_string();
    record_status(home, |st| {
        st.last_restore = Some(StatusRestore {
            at: rfc3339(utc_now()),
            path,
            items,
            mode: rep.mode.clone(),
        })
    });
    Ok(RestoreResult {
        mode: rep.mode,
        added: rep.added,
        conflicts: rep.conflicts,
        removed: rep.removed,
        total_after: rep.total_after,
        safety_backup: safety.map(|p| p.to_string_lossy().to_string()),
    })
}

fn assert_unlocked() -> Result<(), String> {
    if has_vault_pin()? && !is_unlocked() {
        return Err(ERR_BLOQUEADO.to_string());
    }
    Ok(())
}

fn picked(path: Option<tauri_plugin_dialog::FilePath>) -> Result<Option<PathBuf>, String> {
    path.map(|p| p.into_path().map_err(|e| e.to_string()))
        .transpose()
}

#[tauri::command]
pub async fn choose_backup_destination(
    window: tauri::WebviewWindow,
    suggested_name: String,
) -> Result<Option<String>, String> {
    assert_local_origin(&window)?;
    let chosen = window
        .dialog()
        .file()
        .add_filter("Backup do KeyHybrid", &["khvault"])
        .set_file_name(suggested_name)
        .blocking_save_file();
    Ok(picked(chosen)?.map(|mut p| {
        if p.extension().is_none_or(|e| e != "khvault") {
            let mut s = p.into_os_string();
            s.push(".khvault");
            p = PathBuf::from(s);
        }
        p.to_string_lossy().to_string()
    }))
}

#[tauri::command]
pub async fn choose_backup_file(window: tauri::WebviewWindow) -> Result<Option<String>, String> {
    assert_local_origin(&window)?;
    let chosen = window
        .dialog()
        .file()
        .add_filter("Backup do KeyHybrid", &["khvault"])
        .blocking_pick_file();
    Ok(picked(chosen)?.map(|p| p.to_string_lossy().to_string()))
}

#[tauri::command]
pub async fn backup_vault(
    window: tauri::WebviewWindow,
    path: String,
    passphrase: String,
) -> Result<BackupResult, String> {
    assert_local_origin(&window)?;
    assert_unlocked()?;
    let passphrase = Zeroizing::new(passphrase);
    do_backup(&server_bin()?, &khhome(), Path::new(&path), &passphrase)
}

#[tauri::command]
pub async fn inspect_backup(
    window: tauri::WebviewWindow,
    path: String,
    passphrase: String,
) -> Result<BackupPreview, String> {
    assert_local_origin(&window)?;
    assert_unlocked()?;
    let passphrase = Zeroizing::new(passphrase);
    do_inspect(&server_bin()?, &khhome(), Path::new(&path), &passphrase)
}

#[tauri::command]
pub async fn restore_vault(
    window: tauri::WebviewWindow,
    path: String,
    passphrase: String,
    mode: String,
    confirmation: Option<String>,
) -> Result<RestoreResult, String> {
    assert_local_origin(&window)?;
    assert_unlocked()?;
    let passphrase = Zeroizing::new(passphrase);
    do_restore(
        &server_bin()?,
        &khhome(),
        Path::new(&path),
        &passphrase,
        &mode,
        confirmation.as_deref(),
    )
}

#[tauri::command]
pub fn backup_status(window: tauri::WebviewWindow) -> Result<BackupStatus, String> {
    assert_local_origin(&window)?;
    read_status(&khhome())
}

#[cfg(test)]
mod tests {
    use super::*;

    const SENHA: &str = "senha-de-teste-longa";

    fn tmp_dir(nome: &str) -> PathBuf {
        let mut r = [0u8; 8];
        getrandom::fill(&mut r).unwrap();
        let d = std::env::temp_dir().join(format!("kh-backup-{nome}-{:x}", u64::from_le_bytes(r)));
        fs::create_dir_all(&d).unwrap();
        d
    }

    #[test]
    fn escrita_atomica_substitui_e_nao_deixa_part() {
        let d = tmp_dir("atomica");
        let p = d.join("b.khvault");
        write_atomic(&p, b"antigo").unwrap();
        write_atomic(&p, b"novo").unwrap();
        assert_eq!(fs::read(&p).unwrap(), b"novo");
        assert!(!d.join("b.khvault.part").exists());
        // Destino impossível: o .part some e nada fica pela metade
        let ruim = d.join("nao-existe").join("x.khvault");
        assert!(write_atomic(&ruim, b"x").is_err());
        let _ = fs::remove_dir_all(&d);
    }

    #[test]
    fn selar_e_abrir_ida_e_volta_e_erros() {
        let sealed = seal(b"{\"ok\":1}", SENHA).unwrap();
        assert_eq!(&sealed[..8], MAGIC);
        assert_eq!(sealed[8], 1);
        assert_eq!(sealed[9], 0);
        assert_eq!(sealed.len(), HEADER_LEN + 8 + TAG_LEN);
        assert_eq!(open(&sealed, SENHA).unwrap().as_slice(), b"{\"ok\":1}");

        // Salt e nonce novos por arquivo
        let outro = seal(b"{\"ok\":1}", SENHA).unwrap();
        assert_ne!(sealed[10..26], outro[10..26]);
        assert_ne!(sealed[26..HEADER_LEN], outro[26..HEADER_LEN]);
        assert_ne!(sealed[HEADER_LEN..], outro[HEADER_LEN..]);

        assert_eq!(
            open(&sealed, "outra-senha-qualquer").err().unwrap(),
            ERR_SENHA
        );
        // Cabeçalho é AAD: mexer no salt/nonce ou no corpo derruba o GCM
        // salt 10..26, nonce 26..38, primeiro byte cifrado, último byte da tag
        for i in [
            10,
            20,
            25,
            26,
            30,
            HEADER_LEN - 1,
            HEADER_LEN,
            sealed.len() - 1,
        ] {
            let mut t = sealed.clone();
            t[i] ^= 1;
            assert_eq!(open(&t, SENHA).err().unwrap(), ERR_SENHA, "byte {i}");
        }
    }

    #[test]
    fn cabecalho_invalido() {
        let sealed = seal(b"x", SENHA).unwrap();
        assert_eq!(
            open(&sealed[..HEADER_LEN + TAG_LEN - 1], SENHA)
                .err()
                .unwrap(),
            ERR_CORROMPIDO
        );
        let mut t = sealed.clone();
        t[0] = b'X';
        assert_eq!(open(&t, SENHA).err().unwrap(), ERR_CORROMPIDO);
        let mut t = sealed.clone();
        t[8] = 2;
        assert_eq!(open(&t, SENHA).err().unwrap(), ERR_VERSAO_NOVA);
        let mut t = sealed.clone();
        t[8] = 0;
        assert_eq!(open(&t, SENHA).err().unwrap(), ERR_CORROMPIDO);
        let mut t = sealed;
        t[9] = 1;
        assert_eq!(open(&t, SENHA).err().unwrap(), ERR_CORROMPIDO);
    }

    #[test]
    fn senha_curta_conta_caracteres_unicode() {
        assert_eq!(seal(b"x", "curta").err().unwrap(), ERR_SENHA_CURTA);
        // 11 caracteres com acento (mais de 12 bytes) ainda é curta
        assert_eq!(seal(b"x", "ááááááááááá").err().unwrap(), ERR_SENHA_CURTA);
    }

    #[test]
    fn datas_utc() {
        assert_eq!(rfc3339(utc_parts(0)), "1970-01-01T00:00:00Z");
        assert_eq!(rfc3339(utc_parts(951_782_400)), "2000-02-29T00:00:00Z");
        assert_eq!(compact(utc_parts(1_795_000_000)), "20261118-110640");
    }

    #[test]
    fn arquivo_grande_ou_json_invalido() {
        let d = tmp_dir("arq");
        let grande = d.join("grande.khvault");
        fs::File::create(&grande)
            .unwrap()
            .set_len(MAX_FILE + 1)
            .unwrap();
        assert!(
            read_backup(&grande, SENHA)
                .err()
                .unwrap()
                .contains("16 MiB")
        );
        let lixo = d.join("lixo.khvault");
        fs::write(&lixo, seal(b"nao e json", SENHA).unwrap()).unwrap();
        assert_eq!(read_backup(&lixo, SENHA).err().unwrap(), ERR_CORROMPIDO);
        let _ = fs::remove_dir_all(d);
    }

    #[test]
    fn restaurar_substituindo_exige_confirmacao() {
        let d = tmp_dir("conf");
        let p = d.join("x.khvault");
        let bin = &PathBuf::from("/nao/existe");
        let err = do_restore(bin, &d, &p, SENHA, "replace", Some("substituir"))
            .err()
            .unwrap();
        assert_eq!(err, "Para substituir tudo, digite SUBSTITUIR.");
        assert!(do_restore(bin, &d, &p, SENHA, "tudo", None).is_err());
        let _ = fs::remove_dir_all(d);
    }

    #[test]
    fn arquivo_truncado_da_erro_sem_panic() {
        let sealed = seal(b"{\"ok\":1}", SENHA).unwrap();
        for n in [0, 10, 37, 38, 53] {
            assert_eq!(
                open(&sealed[..n], SENHA).err().unwrap(),
                ERR_CORROMPIDO,
                "{n}"
            );
        }
        let n = sealed.len() - 1;
        assert_eq!(open(&sealed[..n], SENHA).err().unwrap(), ERR_SENHA);
    }

    // Cofre em memória com a mesma semântica de vault-dump / vault-apply do Go (sem a regra de ativa)
    #[derive(Default)]
    struct FakeRunner {
        entries: std::cell::RefCell<Vec<serde_json::Value>>,
        dumps: std::cell::Cell<usize>,
        applies: std::cell::Cell<usize>,
    }

    impl FakeRunner {
        fn com(entries: serde_json::Value) -> Self {
            let f = Self::default();
            *f.entries.borrow_mut() = entries.as_array().unwrap().clone();
            f
        }
    }

    impl Runner for FakeRunner {
        fn run(
            &self,
            _home: &Path,
            args: &[&str],
            stdin: Option<&[u8]>,
        ) -> Result<Zeroizing<Vec<u8>>, String> {
            let saida = match args {
                ["vault-dump"] => {
                    self.dumps.set(self.dumps.get() + 1);
                    serde_json::json!({"format":"keyhybrid-vault","version":1,
                        "entries": *self.entries.borrow()})
                }
                [
                    "vault-apply",
                    "--mode",
                    mode @ ("merge" | "replace"),
                    resto @ ..,
                ] => {
                    self.applies.set(self.applies.get() + 1);
                    let dry = resto == ["--dry-run"];
                    let doc: serde_json::Value = serde_json::from_slice(stdin.unwrap()).unwrap();
                    let mut atual = self.entries.borrow().clone();
                    let removed = if *mode == "replace" {
                        std::mem::take(&mut atual).len()
                    } else {
                        0
                    };
                    let (mut added, mut conflicts) = (vec![], vec![]);
                    for e in doc["entries"].as_array().unwrap() {
                        let alias = e["alias"].as_str().unwrap().to_string();
                        if atual.iter().any(|x| x["alias"] == alias.as_str()) {
                            conflicts.push(alias);
                        } else {
                            atual.push(e.clone());
                            added.push(alias);
                        }
                    }
                    let rep = serde_json::json!({"mode":mode,"dry_run":dry,"added":added,
                        "conflicts":conflicts,"removed":removed,"total_after":atual.len()});
                    if !dry {
                        *self.entries.borrow_mut() = atual;
                    }
                    rep
                }
                _ => return Err(format!("comando inesperado: {args:?}")),
            };
            Ok(Zeroizing::new(serde_json::to_vec(&saida).unwrap()))
        }
    }

    fn tres_chaves() -> serde_json::Value {
        serde_json::json!([
            {"alias":"or-teste","provider":"openrouter","kind":"ai","secret":"sk-or-v1-teste-0000000000000000",
             "base_url":"","default_model":"openrouter/auto","active":true,"auth_header":"","auth_prefix":""},
            {"alias":"svc-teste","provider":"supabase","kind":"service","secret":"sbp_teste_0000000000000000",
             "base_url":"https://api.supabase.com","default_model":"","active":false,"auth_header":"","auth_prefix":""},
            {"alias":"svc-custom","provider":"custom","kind":"service","secret":"tok-teste-0000000000000000",
             "base_url":"https://api.exemplo.test","default_model":"","active":false,
             "auth_header":"X-Api-Key","auth_prefix":"Token "}
        ])
    }

    #[test]
    fn backup_e_restauracao_com_runner_falso() {
        let d = tmp_dir("fake");
        let origem = FakeRunner::com(tres_chaves());
        let arquivo = d.join("b.khvault");
        assert_eq!(do_backup(&origem, &d, &arquivo, SENHA).unwrap().items, 3);
        assert_eq!(read_status(&d).unwrap().last_backup.unwrap().items, 3);

        // Prévia é dry-run: não grava nada
        let vazio = FakeRunner::default();
        let pv = do_inspect(&vazio, &d, &arquivo, SENHA).unwrap();
        assert_eq!((pv.total, pv.ai_keys, pv.service_keys), (3, 1, 2));
        assert!(pv.conflicts.is_empty() && vazio.entries.borrow().is_empty());

        // Replace num cofre vazio devolve exatamente as mesmas entries
        let rp = do_restore(&vazio, &d, &arquivo, SENHA, "replace", Some("SUBSTITUIR")).unwrap();
        assert_eq!((rp.added.len(), rp.removed, rp.total_after), (3, 0, 3));
        assert_eq!(*vazio.entries.borrow(), *origem.entries.borrow());
        assert_eq!(
            read_status(&d).unwrap().last_restore.unwrap().mode,
            "replace"
        );

        // Merge com alias em conflito não altera o existente
        let existente = serde_json::json!([{"alias":"or-teste","provider":"openrouter","kind":"ai",
            "secret":"sk-or-v1-teste-1111111111111111","base_url":"","default_model":"outro/modelo",
            "active":false,"auth_header":"","auth_prefix":""}]);
        let cheio = FakeRunner::com(existente.clone());
        let m = do_restore(&cheio, &d, &arquivo, SENHA, "merge", None).unwrap();
        assert_eq!(m.conflicts, ["or-teste"]);
        assert_eq!(m.added, ["svc-teste", "svc-custom"]);
        assert!(m.safety_backup.is_none());
        assert_eq!(cheio.entries.borrow()[0], existente[0]);
        let _ = fs::remove_dir_all(d);
    }

    #[test]
    fn restaurar_com_senha_errada_ou_arquivo_corrompido_nunca_aplica() {
        let d = tmp_dir("nunca");
        let arquivo = d.join("b.khvault");
        do_backup(&FakeRunner::com(tres_chaves()), &d, &arquivo, SENHA).unwrap();
        let mut bytes = fs::read(&arquivo).unwrap();
        *bytes.last_mut().unwrap() ^= 1;
        let corrompido = d.join("c.khvault");
        fs::write(&corrompido, bytes).unwrap();

        let alvo = FakeRunner::com(tres_chaves());
        for (p, senha) in [(&arquivo, "senha-errada-longa"), (&corrompido, SENHA)] {
            for (mode, conf) in [("merge", None), ("replace", Some("SUBSTITUIR"))] {
                let err = do_restore(&alvo, &d, p, senha, mode, conf).err().unwrap();
                assert_eq!(err, ERR_SENHA);
            }
            assert_eq!(do_inspect(&alvo, &d, p, senha).err().unwrap(), ERR_SENHA);
        }
        assert_eq!((alvo.applies.get(), alvo.dumps.get()), (0, 0));
        assert!(!d.join(".keyhybrid").join("backups").exists());
        let _ = fs::remove_dir_all(d);
    }

    #[test]
    fn substituir_guarda_copia_de_seguranca_com_a_mesma_senha() {
        let d = tmp_dir("seg");
        let arquivo = d.join("b.khvault");
        do_backup(&FakeRunner::com(tres_chaves()), &d, &arquivo, SENHA).unwrap();

        let anterior = serde_json::json!([{"alias":"velha","provider":"openrouter","kind":"ai",
            "secret":"sk-or-v1-teste-2222222222222222","base_url":"","default_model":"",
            "active":true,"auth_header":"","auth_prefix":""}]);
        let alvo = FakeRunner::com(anterior.clone());
        let rp = do_restore(&alvo, &d, &arquivo, SENHA, "replace", Some("SUBSTITUIR")).unwrap();
        assert_eq!((rp.removed, rp.total_after), (1, 3));
        let seg = PathBuf::from(rp.safety_backup.unwrap());
        assert!(seg.starts_with(d.join(".keyhybrid").join("backups")));
        assert_eq!(
            read_backup(&seg, SENHA).unwrap().vault.0["entries"],
            anterior
        );
        assert_eq!(
            read_backup(&seg, "senha-errada-longa").err().unwrap(),
            ERR_SENHA
        );
        let _ = fs::remove_dir_all(d);
    }

    #[test]
    fn segredos_em_claro_sao_zerados() {
        let mut v = serde_json::json!({"entries":[{"alias":"or-teste",
            "secret":"sk-or-v1-teste-0000000000000000","active":true}]});
        wipe(&mut v);
        assert_eq!(v["entries"][0]["secret"], "");
        assert_eq!(v["entries"][0]["alias"], "");
        assert_eq!(v["entries"][0]["active"], true);
        assert_eq!(to_json(&v).unwrap().capacity(), MAX_FILE as usize);
    }

    // Backup é só arquivo local + pipe com o servidor: nenhuma API de rede neste módulo
    #[test]
    fn modulo_nao_usa_rede() {
        let fonte = include_str!("backup.rs");
        for proibido in [
            concat!("req", "west"),
            concat!("Tcp", "Stream"),
            concat!("Udp", "Socket"),
            concat!("To", "SocketAddrs"),
        ] {
            assert!(!fonte.contains(proibido), "{proibido}");
        }
    }

    // Integração real com o binário Go: KEYHYBRID_E2E_SERVER_BIN=<caminho> cargo test e2e
    #[test]
    fn e2e_backup_e_restauracao_com_servidor_go() {
        let Ok(bin) = std::env::var("KEYHYBRID_E2E_SERVER_BIN") else {
            eprintln!("e2e pulado: defina KEYHYBRID_E2E_SERVER_BIN");
            return;
        };
        let bin = PathBuf::from(bin);
        let origem = tmp_dir("origem");
        let destino = tmp_dir("destino");
        let doc = serde_json::json!({"format":"keyhybrid-vault","version":1,"entries":[
            {"alias":"or-teste","provider":"openrouter","kind":"ai","secret":"sk-or-v1-teste-0000000000000000",
             "base_url":"","default_model":"openrouter/auto","active":true,"auth_header":"","auth_prefix":""},
            {"alias":"svc-teste","provider":"supabase","kind":"service","secret":"sbp_teste_0000000000000000",
             "base_url":"https://api.supabase.com","default_model":"","active":false,"auth_header":"","auth_prefix":""}]});
        let r = apply_vault(&bin, &origem, &PlainVault(doc), "replace", false).unwrap();
        assert_eq!(r.added, ["or-teste", "svc-teste"]);

        let arquivo = origem.join("b.khvault");
        let b = do_backup(&bin, &origem, &arquivo, SENHA).unwrap();
        assert_eq!(b.items, 2);
        let bytes = fs::read(&arquivo).unwrap();
        assert!(!String::from_utf8_lossy(&bytes).contains("sk-or-v1-teste"));

        let st = read_status(&origem).unwrap();
        assert_eq!(st.last_backup.as_ref().unwrap().items, 2);
        assert!(st.last_restore.is_none());

        // Prévia no mesmo cofre: tudo conflita
        let pv = do_inspect(&bin, &origem, &arquivo, SENHA).unwrap();
        assert_eq!((pv.total, pv.ai_keys, pv.service_keys), (2, 1, 1));
        assert_eq!(pv.providers, ["openrouter", "supabase"]);
        assert_eq!(pv.conflicts, ["or-teste", "svc-teste"]);
        assert_eq!(
            do_inspect(&bin, &origem, &arquivo, "senha-errada-longa")
                .err()
                .unwrap(),
            ERR_SENHA
        );

        // Merge num cofre vazio adiciona tudo
        let m = do_restore(&bin, &destino, &arquivo, SENHA, "merge", None).unwrap();
        assert_eq!(
            (m.added.len(), m.total_after, m.safety_backup.is_none()),
            (2, 2, true)
        );
        let m2 = do_restore(&bin, &destino, &arquivo, SENHA, "merge", None).unwrap();
        assert_eq!((m2.added.len(), m2.conflicts.len()), (0, 2));

        // Replace guarda a cópia de segurança antes e devolve o cofre idêntico
        let rp = do_restore(
            &bin,
            &destino,
            &arquivo,
            SENHA,
            "replace",
            Some(" SUBSTITUIR "),
        )
        .unwrap();
        assert_eq!((rp.removed, rp.total_after), (2, 2));
        let seg = PathBuf::from(rp.safety_backup.unwrap());
        assert!(seg.starts_with(destino.join(".keyhybrid").join("backups")));
        assert_eq!(
            read_backup(&seg, SENHA).unwrap().vault,
            dump_vault(&bin, &destino).unwrap()
        );
        assert_eq!(
            dump_vault(&bin, &destino).unwrap(),
            dump_vault(&bin, &origem).unwrap()
        );
        assert_eq!(
            read_status(&destino).unwrap().last_restore.unwrap().mode,
            "replace"
        );

        let _ = fs::remove_dir_all(origem);
        let _ = fs::remove_dir_all(destino);
    }
}
