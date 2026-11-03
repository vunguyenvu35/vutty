# Cask for the tap github.com/vunguyenvu35/homebrew-vutty (copy this file to Casks/vutty.rb there).
# On each release: change `version` and `sha256` (the line for vutty-<version>-macos.zip in the release's SHA256SUMS).
cask "vutty" do
  version "1.2.0"
  sha256 "04da90e39a4eac0138a17fca06703c289ec11820a1474da2af195607cabc9180"

  url "https://github.com/vunguyenvu35/vutty/releases/download/v#{version}/vutty-#{version}-macos.zip"
  name "Vutty"
  desc "SSH client with an encrypted vault and Git sync"
  homepage "https://github.com/vunguyenvu35/vutty"

  depends_on macos: ">= :high_sierra"

  app "vutty.app"

  # Beta builds are not notarized yet: without this, Gatekeeper refuses to open the app.
  postflight do
    system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{appdir}/vutty.app"]
  end

  zap trash: "~/Library/Application Support/vutty"
end
