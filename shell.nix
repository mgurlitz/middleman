{ pkgs ? import <nixpkgs> {} }:

let
  go = if pkgs ? go_1_26 then pkgs.go_1_26 else pkgs.go;
  node =
    if pkgs ? nodejs_24 then pkgs.nodejs_24
    else if pkgs ? nodejs_22 then pkgs.nodejs_22
    else pkgs.nodejs;
in
pkgs.mkShell {
  packages = with pkgs; [
    go
    bun
    node
    gnumake
    git
    cargo
    rustc
    cacert
  ];

  CGO_ENABLED = "0";
  SSL_CERT_FILE = "${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt";

  shellHook = ''
    echo "middleman nix shell"
    echo "Run: bun install && make build"
  '';
}
