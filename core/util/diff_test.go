package util

import "testing"

func diffCounts(changes []DiffChange) (additions, deletions int) {
	for _, c := range changes {
		if c.Added {
			additions += c.Count
		}
		if c.Removed {
			deletions += c.Count
		}
	}
	return additions, deletions
}

func TestDiffLines(t *testing.T) {
	cases := []struct {
		name           string
		oldStr, newStr string
		additions      int
		deletions      int
	}{
		{"single line change", "a\nb\nc\n", "a\nB\nc\n", 1, 1},
		{"middle line", "line1\nline2\nline3\nline4\nline5\n", "line1\nline2\nCHANGED\nline4\nline5\n", 1, 1},
		{"no trailing newline", "a\nb", "a\nc", 1, 1},
		{"append line", "hello\n", "hello\nworld\n", 1, 0},
		{"remove newline", "x\n", "x", 1, 1},
		{"context hunk", "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\n", "a\nb\nc\nd\ne\nF\ng\nh\ni\nj\nk\n", 1, 1},
		{"full replace", "foo", "bar", 1, 1},
		{"identical", "same\n", "same\n", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			additions, deletions := diffCounts(DiffLines(tc.oldStr, tc.newStr))
			if additions != tc.additions || deletions != tc.deletions {
				t.Fatalf("got +%d -%d, want +%d -%d", additions, deletions, tc.additions, tc.deletions)
			}
		})
	}
}

func TestCreateTwoFilesPatch(t *testing.T) {
	cases := []struct {
		name           string
		oldStr, newStr string
		want           string
	}{
		{
			name:   "single line change",
			oldStr: "a\nb\nc\n",
			newStr: "a\nB\nc\n",
			want: "Index: f.txt\n" +
				"===================================================================\n" +
				"--- f.txt\n" +
				"+++ f.txt\n" +
				"@@ -1,3 +1,3 @@\n" +
				" a\n" +
				"-b\n" +
				"+B\n" +
				" c\n",
		},
		{
			name:   "no trailing newline",
			oldStr: "a\nb",
			newStr: "a\nc",
			want: "Index: f.txt\n" +
				"===================================================================\n" +
				"--- f.txt\n" +
				"+++ f.txt\n" +
				"@@ -1,2 +1,2 @@\n" +
				" a\n" +
				"-b\n" +
				"\\ No newline at end of file\n" +
				"+c\n" +
				"\\ No newline at end of file\n",
		},
		{
			name:   "append line",
			oldStr: "hello\n",
			newStr: "hello\nworld\n",
			want: "Index: f.txt\n" +
				"===================================================================\n" +
				"--- f.txt\n" +
				"+++ f.txt\n" +
				"@@ -1,1 +1,2 @@\n" +
				" hello\n" +
				"+world\n",
		},
		{
			name:   "context hunk",
			oldStr: "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\n",
			newStr: "a\nb\nc\nd\ne\nF\ng\nh\ni\nj\nk\n",
			want: "Index: f.txt\n" +
				"===================================================================\n" +
				"--- f.txt\n" +
				"+++ f.txt\n" +
				"@@ -2,9 +2,9 @@\n" +
				" b\n" +
				" c\n" +
				" d\n" +
				" e\n" +
				"-f\n" +
				"+F\n" +
				" g\n" +
				" h\n" +
				" i\n" +
				" j\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CreateTwoFilesPatch("f.txt", "f.txt", tc.oldStr, tc.newStr)
			if got != tc.want {
				t.Fatalf("patch mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, tc.want)
			}
		})
	}
}
