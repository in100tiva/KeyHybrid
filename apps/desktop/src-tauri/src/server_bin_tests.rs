// Testes canônicos de `server_bin` (issue #16). Não editar: o gate confere o hash.
// Rodam sem Tauri/WebKit: `rustc --edition 2024 --test src/server_bin.rs`.
use super::*;
use std::fs;
use std::path::{Path, PathBuf};

fn p(s: &str) -> PathBuf {
    PathBuf::from(s)
}

#[test]
fn nome_do_binario_por_plataforma() {
    if cfg!(target_os = "windows") {
        assert_eq!(bin_name(), "keyhybrid-server.exe");
    } else {
        assert_eq!(bin_name(), "keyhybrid-server");
    }
}

#[test]
fn sem_resource_dir_mantem_os_7_candidatos_na_ordem_antiga() {
    let got = candidates(
        "kh",
        Path::new("/exe"),
        Path::new("/cwd"),
        Path::new("/home/u"),
        None,
    );
    let want = vec![
        p("/exe/kh"),
        p("/exe/server/kh"),
        p("/cwd/kh"),
        p("/cwd/apps/server/kh"),
        p("/cwd/../server/kh"),
        p("/cwd/../../server/kh"),
        p("/home/u/.keyhybrid/bin/kh"),
    ];
    assert_eq!(got, want);
}

#[test]
fn com_resource_dir_o_servidor_empacotado_entra_em_terceiro() {
    // .deb: binário em /usr/bin, recursos em /usr/lib/KeyHybrid/server/.
    let got = candidates(
        "kh",
        Path::new("/usr/bin"),
        Path::new("/cwd"),
        Path::new("/home/u"),
        Some(Path::new("/usr/lib/KeyHybrid")),
    );
    let want = vec![
        p("/usr/bin/kh"),
        p("/usr/bin/server/kh"),
        p("/usr/lib/KeyHybrid/server/kh"),
        p("/cwd/kh"),
        p("/cwd/apps/server/kh"),
        p("/cwd/../server/kh"),
        p("/cwd/../../server/kh"),
        p("/home/u/.keyhybrid/bin/kh"),
    ];
    assert_eq!(got, want);
}

#[test]
fn first_existing_devolve_o_primeiro_que_existe_no_disco() {
    let base = std::env::temp_dir().join(format!("kh-server-bin-{}", std::process::id()));
    let _ = fs::remove_dir_all(&base);
    let res = base.join("lib").join("server");
    let home = base.join("home").join(".keyhybrid").join("bin");
    fs::create_dir_all(&res).unwrap();
    fs::create_dir_all(&home).unwrap();
    fs::write(res.join("kh"), b"x").unwrap();
    fs::write(home.join("kh"), b"x").unwrap();

    let cands = candidates(
        "kh",
        &base.join("nao-existe-exe"),
        &base.join("nao-existe-cwd"),
        &base.join("home"),
        Some(&base.join("lib")),
    );
    assert_eq!(first_existing(&cands), Some(res.join("kh")));

    // Sem o recurso empacotado, cai no ~/.keyhybrid/bin.
    let sem_res = candidates(
        "kh",
        &base.join("nao-existe-exe"),
        &base.join("nao-existe-cwd"),
        &base.join("home"),
        None,
    );
    assert_eq!(first_existing(&sem_res), Some(home.join("kh")));

    // Diretório com o nome do binário não conta como binário.
    fs::remove_file(res.join("kh")).unwrap();
    fs::create_dir_all(res.join("kh")).unwrap();
    assert_eq!(first_existing(&cands), Some(home.join("kh")));

    let nada = candidates(
        "kh",
        &base.join("a"),
        &base.join("b"),
        &base.join("c"),
        None,
    );
    assert_eq!(first_existing(&nada), None);

    let _ = fs::remove_dir_all(&base);
}
