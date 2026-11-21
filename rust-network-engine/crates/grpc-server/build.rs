//! Build script: compile the shared protobuf contracts into Rust.
//!
//! The contracts live in `proto/cloudforge/v1/` at the repo root and are
//! shared with the Go control plane — they are **not** redefined here.
//!
//! `protoc` resolution, in order:
//! 1. `$PROTOC` if set in the environment.
//! 2. `protoc` on `PATH` (e.g. `apt install protobuf-compiler`).
//! 3. A vendored `protoc` binary from the `protoc-bin-vendored` crate,
//!    so `cargo build` works on a machine without a system protoc.
//!
//! Generated code is produced with `tonic-build` 0.12 (`compile_protos`).

use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto_root = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("..")
        .join("..")
        .join("proto");
    let network_proto = proto_root.join("cloudforge/v1/network.proto");
    let common_proto = proto_root.join("cloudforge/v1/common.proto");
    for proto in [&network_proto, &common_proto] {
        println!("cargo:rerun-if-changed={}", proto.display());
    }

    if std::env::var_os("PROTOC").is_none()
        && std::process::Command::new("protoc")
            .arg("--version")
            .output()
            .is_err()
    {
        // No system protoc: fall back to the vendored binary.
        let vendored = protoc_bin_vendored::protoc_bin_path()?;
        std::env::set_var("PROTOC", vendored);
    }

    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        .compile_protos(&[network_proto], &[proto_root])?;
    Ok(())
}
