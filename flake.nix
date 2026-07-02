{
  description = "Crush development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
  }:
    flake-utils.lib.eachDefaultSystem (
      system: let
        pkgs = nixpkgs.legacyPackages.${system};
        version = "devel";
      in {
        packages.default = pkgs.buildGoModule {
          pname = "crush";
          inherit version;

          src = ./.;

          vendorHash = "sha256-4gHvyEqiFhEvZ90lJbXeI/1fMMo6L19P/PD5Eu5YUmI=";

          # Match the Go toolchain used in the dev shell.
          go = pkgs.go_1_26;

          env.CGO_ENABLED = 0;

          ldflags = [
            "-s"
            "-w"
            "-X github.com/charmbracelet/crush/internal/version.Version=${version}"
          ];

          # The test suite requires network access and external tools.
          doCheck = false;

          meta = {
            description = "Terminal-based AI coding assistant";
            homepage = "https://github.com/charmbracelet/crush";
            license = pkgs.lib.licenses.mit;
            mainProgram = "crush";
          };
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            # Go toolchain
            go_1_26

            # Development tools
            gopls # Go language server
            golangci-lint # Linter
            gofumpt # Formatter (stricter than gofmt)
            go-task # Task runner
            delve # Go debugger

            # Additional tools
            git # Version control
            gh # GitHub CLI
            svu # Semantic version utility
            sqlc # SQL code generator
          ];

          shellHook = ''
            # Set Go environment variables
            export CGO_ENABLED=0
          '';
        };
      }
    );
}
