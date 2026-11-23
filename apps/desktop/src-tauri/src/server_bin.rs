use std::path::{Path, PathBuf};

pub fn bin_name() -> &'static str {
    if cfg!(target_os = "windows") {
        "keyhybrid-server.exe"
    } else {
        "keyhybrid-server"
    }
}

pub fn candidates(
    bin_name: &str,
    exe_dir: &Path,
    cwd: &Path,
    home_dir: &Path,
    resource_dir: Option<&Path>,
) -> Vec<PathBuf> {
    let mut list = vec![
        exe_dir.join(bin_name),
        exe_dir.join("server").join(bin_name),
    ];
    if let Some(res) = resource_dir {
        list.push(res.join("server").join(bin_name));
    }
    list.extend([
        cwd.join(bin_name),
        cwd.join("apps").join("server").join(bin_name),
        cwd.join("..").join("server").join(bin_name),
        cwd.join("..").join("..").join("server").join(bin_name),
        home_dir.join(".keyhybrid").join("bin").join(bin_name),
    ]);
    list
}

pub fn first_existing(candidates: &[PathBuf]) -> Option<PathBuf> {
    candidates.iter().find(|p| p.is_file()).cloned()
}

#[cfg(test)]
#[path = "server_bin_tests.rs"]
mod tests;
