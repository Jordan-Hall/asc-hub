// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascdownload

import (
	"net/url"
	"regexp"
	"strings"
)

// Platform is the operating system and processor of a visitor. A zero OS means "not known".
type Platform struct {
	OS   string
	Arch string
}

// DetectPlatform reads a User-Agent header. It is only a first guess for the page the server renders; the
// page's script refines the processor with the browser's own client hints. A phone or tablet gets no OS,
// because the command line does not run there.
func DetectPlatform(userAgent string) Platform {
	ua := strings.ToLower(userAgent)
	p := Platform{}
	switch {
	case ua == "":
	case strings.Contains(ua, "android"), strings.Contains(ua, "iphone"), strings.Contains(ua, "ipad"), strings.Contains(ua, "cros"):
	case strings.Contains(ua, "windows"):
		p.OS = OSWindows
	case strings.Contains(ua, "macintosh"), strings.Contains(ua, "mac os x"):
		p.OS = OSMacOS
	case strings.Contains(ua, "linux"), strings.Contains(ua, "x11"), strings.Contains(ua, "freebsd"):
		p.OS = OSLinux
	}
	switch {
	case p.OS == "":
	case strings.Contains(ua, "arm64"), strings.Contains(ua, "aarch64"), strings.Contains(ua, "armv8"):
		p.Arch = ArchArm64
	case p.OS == OSMacOS:
		// Safari and Chrome on Apple silicon still say "Intel", so the guess is the common machine today.
		p.Arch = ArchArm64
	default:
		p.Arch = ArchX86_64
	}
	return p
}

// FileView is a listed file with the links to fetch it.
type FileView struct {
	File
	URL          string
	SignatureURL string
}

// ArchOffer is what is offered for one processor of one operating system.
type ArchOffer struct {
	Arch     string
	Label    string
	Primary  *FileView
	Files    []FileView
	Commands []Command
}

// Command is a block of shell lines the visitor can paste, with the shell it is written for.
type Command struct {
	Title string
	Shell string // "powershell" or "sh"
	Text  string
}

// OSOffer is everything offered for one operating system.
type OSOffer struct {
	OS              string
	Label           string
	Arches          []ArchOffer
	PackageCommands []Command
}

// Offers is the page model of a release.
type Offers struct {
	Manifest       *Manifest
	OSes           []OSOffer
	Checksums      *FileView
	ChecksumsSig   *FileView
	VerifyCommand  string
	ConnectCommand string
}

// FileURL is the address of a file below base. The name is already validated, and it is escaped anyway.
func FileURL(base, name string) string {
	return base + url.PathEscape(name)
}

var osLabels = map[string]string{OSWindows: "Windows", OSMacOS: "macOS", OSLinux: "Linux"}
var archLabels = map[string]string{ArchX86_64: "x86_64 (Intel, AMD)", ArchArm64: "arm64 (Apple silicon, ARM)"}

// BuildOffers turns a manifest into the page model. base is the link base of the files (it ends in a
// slash) and hubURL the root URL of this hub, used for the connect command.
func BuildOffers(m *Manifest, base, hubURL string) *Offers {
	o := &Offers{Manifest: m}
	view := func(f File) FileView {
		v := FileView{File: f, URL: FileURL(base, f.Name)}
		if f.Signature != "" {
			v.SignatureURL = FileURL(base, f.Signature)
		}
		return v
	}
	for _, osName := range []string{OSWindows, OSMacOS, OSLinux} {
		offer := OSOffer{OS: osName, Label: osLabels[osName]}
		for _, arch := range []string{ArchX86_64, ArchArm64} {
			var files []FileView
			for _, f := range m.Files {
				if f.OS == osName && (f.Arch == arch || f.Arch == ArchAny) {
					files = append(files, view(f))
				}
			}
			if len(files) == 0 {
				continue
			}
			a := ArchOffer{Arch: arch, Label: archLabels[arch], Files: files}
			for i := range files {
				if files[i].Kind != KindScript {
					a.Primary = &a.Files[i]
					break
				}
			}
			if a.Primary == nil {
				// only a script that suits every processor: there is no build to install with it
				continue
			}
			a.Commands = installCommands(osName, a)
			offer.Arches = append(offer.Arches, a)
		}
		if len(offer.Arches) == 0 {
			continue
		}
		offer.PackageCommands = packageCommands(osName, m)
		o.OSes = append(o.OSes, offer)
	}
	if m.Checksums != "" {
		f := FileView{File: File{Name: m.Checksums}, URL: FileURL(base, m.Checksums)}
		o.Checksums = &f
		if m.ChecksumsSignature != "" {
			s := FileView{File: File{Name: m.ChecksumsSignature}, URL: FileURL(base, m.ChecksumsSignature)}
			o.ChecksumsSig = &s
			if m.MinisignPublicKey != "" {
				o.VerifyCommand = "minisign -Vm " + shQuote(m.Checksums) + " -x " + shQuote(m.ChecksumsSignature) + " -P " + shQuote(m.MinisignPublicKey)
			}
		}
	}
	o.ConnectCommand = ConnectCommand(hubURL)
	return o
}

// Pick chooses the offer for a platform: its operating system if the release has one, with the processor if
// there is a build for it, else the first processor there is. The second result is false when the release
// has nothing for the operating system.
func (o *Offers) Pick(p Platform) (*OSOffer, *ArchOffer, bool) {
	for i := range o.OSes {
		if o.OSes[i].OS != p.OS {
			continue
		}
		os := &o.OSes[i]
		for j := range os.Arches {
			if os.Arches[j].Arch == p.Arch {
				return os, &os.Arches[j], true
			}
		}
		return os, &os.Arches[0], true
	}
	return nil, nil, false
}

var safeWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shQuote quotes one word for a POSIX shell.
func shQuote(s string) string {
	if safeWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// psQuote quotes one word for PowerShell. A single quoted string takes everything literally except the
// quote itself, which is doubled.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// ConnectCommand is the command that signs the asc command line in to this hub. The host is the hub's own
// root URL, never a fixed host.
func ConnectCommand(hubURL string) string {
	return "asc auth login --host " + shQuote(strings.TrimRight(hubURL, "/")) + " --prompt"
}

func packageCommands(osName string, m *Manifest) []Command {
	var out []Command
	pm := m.PackageManagers
	if v := pm["winget"]; v != "" && osName == OSWindows {
		out = append(out, Command{Title: "winget", Shell: "powershell", Text: "winget install --exact --id " + shQuote(v)})
	}
	if v := pm["scoop"]; v != "" && osName == OSWindows {
		out = append(out, Command{Title: "Scoop", Shell: "powershell", Text: "scoop install " + shQuote(v)})
	}
	if v := pm["brew"]; v != "" && osName != OSWindows {
		out = append(out, Command{Title: "Homebrew", Shell: "sh", Text: "brew install " + shQuote(v)})
	}
	return out
}

// installCommands writes the commands that download one build, check its SHA-256 against the manifest and
// install it. The checksum is compared before anything runs, and the command stops if it differs.
func installCommands(osName string, a ArchOffer) []Command {
	p := a.Primary
	var cmds []Command
	switch osName {
	case OSWindows:
		if p.Kind == KindInstaller {
			cmds = append(cmds, Command{Title: "PowerShell", Shell: "powershell", Text: strings.Join([]string{
				"$f = " + psQuote(p.Name),
				"Invoke-WebRequest -Uri " + psQuote(p.URL) + " -OutFile $f",
				"if ((Get-FileHash $f -Algorithm SHA256).Hash -ne " + psQuote(strings.ToUpper(p.SHA256)) + ") { Remove-Item $f; throw 'checksum mismatch, nothing was installed' }",
				"Start-Process msiexec.exe -ArgumentList '/i', $f, '/qb' -Wait",
			}, "\n")})
		} else {
			cmds = append(cmds, Command{Title: "PowerShell", Shell: "powershell", Text: strings.Join([]string{
				"$f = " + psQuote(p.Name),
				"Invoke-WebRequest -Uri " + psQuote(p.URL) + " -OutFile $f",
				"if ((Get-FileHash $f -Algorithm SHA256).Hash -ne " + psQuote(strings.ToUpper(p.SHA256)) + ") { Remove-Item $f; throw 'checksum mismatch, nothing was unpacked' }",
				"Expand-Archive $f -DestinationPath \"$env:LOCALAPPDATA\\asc\" -Force",
			}, "\n")})
		}
	case OSMacOS:
		sum := "echo " + shQuote(p.SHA256+"  "+p.Name) + " | shasum -a 256 -c -"
		if p.Kind == KindInstaller {
			cmds = append(cmds, Command{Title: "Terminal", Shell: "sh", Text: strings.Join([]string{
				"curl -fsSLO " + shQuote(p.URL),
				sum,
				"sudo installer -pkg " + shQuote(p.Name) + " -target /",
			}, "\n")})
		} else {
			cmds = append(cmds, Command{Title: "Terminal", Shell: "sh", Text: strings.Join([]string{
				"curl -fsSLO " + shQuote(p.URL),
				sum,
				"tar -xzf " + shQuote(p.Name) + " asc",
				"install -m 755 asc \"$HOME/.local/bin/asc\"",
			}, "\n")})
		}
	case OSLinux:
		sum := "echo " + shQuote(p.SHA256+"  "+p.Name) + " | sha256sum -c -"
		var script *FileView
		for i := range a.Files {
			if a.Files[i].Kind == KindScript {
				script = &a.Files[i]
			}
		}
		if script != nil && p.SignatureURL != "" {
			cmds = append(cmds, Command{Title: "Terminal", Shell: "sh", Text: strings.Join([]string{
				"curl -fsSLO " + shQuote(script.URL) + " -O " + shQuote(p.URL) + " -O " + shQuote(p.SignatureURL),
				sum,
				"sh " + shQuote(script.Name) + " --tarball " + shQuote(p.Name) + " --sig " + shQuote(p.Signature),
			}, "\n")})
		} else {
			cmds = append(cmds, Command{Title: "Terminal", Shell: "sh", Text: strings.Join([]string{
				"curl -fsSLO " + shQuote(p.URL),
				sum,
				"tar -xzf " + shQuote(p.Name) + " asc",
				"install -m 755 asc \"$HOME/.local/bin/asc\"",
			}, "\n")})
		}
	}
	return cmds
}
