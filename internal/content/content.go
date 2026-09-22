package content

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var RequiredModels = []string{
	"models/alliance_roles.yaml",
	"models/area_effects.yaml",
	"models/augments.yaml",
	"models/battle_pass.yaml",
	"models/character_progression.yaml",
	"models/character_stats.yaml",
	"models/characters.yaml",
	"models/currencies.yaml",
	"models/dashes.yaml",
	"models/fingerprint.yaml",
	"models/gacha_boxes.yaml",
	"models/game_modes.yaml",
	"models/items.yaml",
	"models/locations.yaml",
	"models/location_themes.yaml",
	"models/map_tiles.yaml",
	"models/maps.yaml",
	"models/missions.yaml",
	"models/projectiles.yaml",
	"models/shop_offers.yaml",
	"models/skills.yaml",
	"models/skins.yaml",
	"models/star_powers.yaml",
	"models/ultimate_upgrades.yaml",
}

type File struct {
	Path    string
	Content []byte
}

type Catalog struct {
	Root   string
	Files  []File
	ByPath map[string][]byte
}

func resolveRoot(configured string) string { return configured }

func Load(configuredRoot string) *Catalog {
	root := resolveRoot(configuredRoot)
	c := &Catalog{Root: root, ByPath: map[string][]byte{}}
	for _, rel := range RequiredModels {
		p := filepath.Join(root, filepath.FromSlash(rel))
		b, err := os.ReadFile(p)
		if err != nil {
			b = []byte("# synthetic stub for " + rel + "\n")
		}
		c.Files = append(c.Files, File{Path: rel, Content: b})
		c.ByPath[rel] = b
	}

	var fingerprint strings.Builder
	fingerprint.WriteString("files:\n")
	for _, f := range c.Files {
		if f.Path != "models/fingerprint.yaml" {
			fingerprint.WriteString("- " + f.Path + "\n")
		}
	}
	fingerprint.WriteString("checksum: " + c.FingerprintChecksum() + "\n")
	data := []byte(fingerprint.String())
	c.ByPath["models/fingerprint.yaml"] = data
	for i := range c.Files {
		if c.Files[i].Path == "models/fingerprint.yaml" {
			c.Files[i].Content = data
		}
	}
	return c
}

// FingerprintChecksum computes SHA256 over [pathLen BE32][path][fileLen BE32][bytes]
// in Files order — same as C# Fingerprint.Calculate.
func (c *Catalog) FingerprintChecksum() string {
	h := sha256.New()
	var tmp [4]byte
	for _, f := range c.Files {
		if f.Path == "models/fingerprint.yaml" {
			continue
		}
		binary.BigEndian.PutUint32(tmp[:], uint32(len(f.Path)))
		h.Write(tmp[:])
		h.Write([]byte(f.Path))
		binary.BigEndian.PutUint32(tmp[:], uint32(len(f.Content)))
		h.Write(tmp[:])
		h.Write(f.Content)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (c *Catalog) FingerprintFile() File {
	for _, f := range c.Files {
		if f.Path == "models/fingerprint.yaml" {
			return f
		}
	}
	return File{Path: "models/fingerprint.yaml", Content: []byte("checksum: " + c.FingerprintChecksum() + "\n")}
}

func (c *Catalog) SortedFiles() []File {
	out := append([]File(nil), c.Files...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func IsSyntheticMode(mode string) bool {
	return !strings.EqualFold(mode, "Imported")
}

func SyntheticCharacters() []SyntheticCharacter {
	return []SyntheticCharacter{
		{ID: 1, Name: "synthetic_brawler", HP: 3600, Speed: 720, Weapon: 1, Ult: 2},
		{ID: 2, Name: "synthetic_caster", HP: 2800, Speed: 760, Weapon: 3, Ult: 4},
		{ID: 3, Name: "synthetic_tank", HP: 5200, Speed: 640, Weapon: 5, Ult: 6},
	}
}

type SyntheticCharacter struct {
	ID     int
	Name   string
	HP     int
	Speed  int
	Weapon int
	Ult    int
}
