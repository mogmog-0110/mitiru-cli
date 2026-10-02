package commands

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
)

// packSkipDir は pack に入れずバラ置きで残す assets/ 直下のディレクトリ。
// RmlUi は文書と RCSS をファイルから直に読み、pack を見ない。
const packSkipDir = "ui"

// distDiskReadExt は engine がファイルから直に読む (pack を見ない) 種類。3D モデルの取り込み
// (.clod の変換と BC 圧縮の .dds を元ファイルの隣に作る)、当たり判定のメッシュ、ナビメッシュがこれに当たる。
// pack へ移すと、描画も当たり判定も黙って空になる。
var distDiskReadExt = map[string]bool{
	".obj": true, ".mtl": true, ".gltf": true, ".glb": true, ".bin": true, ".fbx": true,
	".clod": true, ".dds": true, ".navmesh": true, ".navcache": true,
}

// keepLooseInDist は assets/ からの相対パス rel を pack に入れずバラ置きで残すか返す。
func keepLooseInDist(rel string) bool {
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, packSkipDir+"/") {
		return true
	}
	return distDiskReadExt[strings.ToLower(filepath.Ext(rel))]
}

// removePackedAssets は pack に畳んだファイルを消し、空になったディレクトリも消す。
func removePackedAssets(assetsDir string, packed []string) error {
	for _, p := range packed {
		if err := os.Remove(p); err != nil {
			return err
		}
	}
	var dirs []string
	err := filepath.Walk(assetsDir, func(path string, info os.FileInfo, werr error) error {
		if werr == nil && info.IsDir() && path != assetsDir {
			dirs = append(dirs, path)
		}
		return werr
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- { // 深い方から
		if entries, rerr := os.ReadDir(dirs[i]); rerr == nil && len(entries) == 0 {
			if err := os.Remove(dirs[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// packAssets は assetsDir 以下 (keepLooseInDist を除く) を再帰的に読み、keyPrefix を前置したキーで
// .mtpak に書き出す。キーは host / native loader が要求する cwd 相対パスに一致させる。
// pack に入れたファイルの絶対パスと、バラ置きで残した数を返す。
func packAssets(assetsDir, outFile, keyPrefix string) ([]string, int, error) {
	var keys, packed []string
	var datas [][]byte
	loose := 0
	err := filepath.Walk(assetsDir, func(path string, info os.FileInfo, werr error) error {
		if werr != nil || info.IsDir() {
			return werr
		}
		rel, rerr := filepath.Rel(assetsDir, path)
		if rerr != nil {
			return rerr
		}
		if keepLooseInDist(rel) {
			loose++
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		keys = append(keys, keyPrefix+"/"+filepath.ToSlash(rel))
		datas = append(datas, data)
		packed = append(packed, path)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if len(keys) == 0 {
		return nil, loose, nil // 畳むものが無ければ pack を作らない
	}
	if err := writeAssetPack(outFile, keys, datas, true); err != nil {
		return nil, 0, err
	}
	return packed, loose, nil
}

// writeAssetPack は AssetPack.hpp (ADR 0016) と **バイト互換**の .mtpak を書く。
// 形式: magic"MTPAK\0" | version u16 | flags u16 | count u32 |
//
//	[count] keyLen u16, key, offset u64, size u64 | blob region (scramble 時 XOR)。
//
// C++ 側 (mitiru::vfs::AssetPack::open/read) がこれを読むので、両者の形式は一致必須。
func writeAssetPack(outFile string, keys []string, datas [][]byte, scramble bool) error {
	blobStart := uint64(6 + 2 + 2 + 4)
	for _, k := range keys {
		blobStart += uint64(2 + len(k) + 8 + 8)
	}
	var buf bytes.Buffer
	buf.WriteString("MTPAK\x00")
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // version
	var flags uint16
	if scramble {
		flags = 1
	}
	_ = binary.Write(&buf, binary.LittleEndian, flags)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(keys)))
	off := blobStart
	offsets := make([]uint64, len(keys))
	for i, k := range keys {
		_ = binary.Write(&buf, binary.LittleEndian, uint16(len(k)))
		buf.WriteString(k)
		_ = binary.Write(&buf, binary.LittleEndian, off)
		_ = binary.Write(&buf, binary.LittleEndian, uint64(len(datas[i])))
		offsets[i] = off
		off += uint64(len(datas[i]))
	}
	for i := range keys {
		d := datas[i]
		if scramble {
			d = make([]byte, len(datas[i]))
			copy(d, datas[i])
			for j := range d {
				d[j] ^= byte(0x5A + ((offsets[i] + uint64(j)) & 0xFF))
			}
		}
		buf.Write(d)
	}
	return os.WriteFile(outFile, buf.Bytes(), 0o644)
}
