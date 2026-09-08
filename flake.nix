{
  description = "Go Linux build workspace on macOS";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [
        "aarch64-darwin"
        "x86_64-darwin"
      ];

      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      devShells = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              go
              gopls
              gotools
              golangci-lint
              delve
              git
              gnumake
            ];

            env = {
              CGO_ENABLED = "0";
              GOOS = "linux";
            };

            shellHook = ''
              echo "Go Linux workspace"
              echo "GOOS=$GOOS"
              echo "CGO_ENABLED=$CGO_ENABLED"
              go version
            '';
          };
        });
    };
}
