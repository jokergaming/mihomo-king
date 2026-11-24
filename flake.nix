{
  description = "TUI and CLI for managing mihomo, sing-box, and Xray proxy cores";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
    in {
      packages = nixpkgs.lib.genAttrs systems (system:
        let
          pkgs = import nixpkgs { inherit system; };
          mihomo-king = pkgs.buildGoModule {
            pname = "mihomo-king";
            version = self.shortRev or self.dirtyShortRev or "dev";
            src = self;
            subPackages = [ "." ];
            vendorHash = pkgs.lib.fakeHash;

            env.CGO_ENABLED = 0;
            ldflags = [ "-s" "-w" "-X mihomo-king/internal/cli.Version=${self.shortRev or self.dirtyShortRev or "dev"}" ];

            # Tests execute temporary scripts with /bin/sh from the host.
            doCheck = false;
            nativeBuildInputs = [ pkgs.installShellFiles ];
            postInstall = ''
              installShellCompletion --cmd mihomo-king \
                --bash <($out/bin/mihomo-king completion bash) \
                --zsh <($out/bin/mihomo-king completion zsh)
            '';

            meta.mainProgram = "mihomo-king";
          };
        in {
          inherit mihomo-king;
          default = mihomo-king;
        });
    };
}
