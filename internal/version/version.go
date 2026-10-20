// Package version 提供应用版本号与版本比较。
//
// 版本号在编译期通过 -ldflags "-X wallpaper-tool/internal/version.Version=x.y.z" 注入,
// 未注入时为 "dev"(开发构建)。
package version

import (
	"strconv"
	"strings"
)

// Version 由编译期注入, 勿在运行期修改。
var Version = "dev"

// Current 返回当前版本号。
func Current() string {
	return Version
}

// Compare 比较两个语义化版本号(形如 1.2.3, 可带 v 前缀)。
// 返回 -1 表示 a < b, 0 表示相等, 1 表示 a > b。
// 无法解析的片段按 0 处理, 多余的预发布后缀(-beta 等)被忽略。
func Compare(a, b string) int {
	av := parse(a)
	bv := parse(b)
	for i := 0; i < 3; i++ {
		if av[i] != bv[i] {
			if av[i] < bv[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// parse 解析版本号的主版本/次版本/修订号。
func parse(v string) [3]int {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	// 去掉预发布/构建后缀: 1.2.3-beta.1 -> 1.2.3
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, part := range strings.Split(v, ".") {
		if i > 2 {
			break
		}
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		out[i] = n
	}
	return out
}
