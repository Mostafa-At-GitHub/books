package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"html/template"

	"github.com/essentialbooks/books/pkg/common"
	"github.com/kjk/notionapi"
	"github.com/kjk/u"
	"github.com/tdewolff/minify"
)

var (
	flgAnalytics string
	flgPreview   bool
	flgNoCache   bool

	soUserIDToNameMap map[int]string
	googleAnalytics   template.HTML
	doMinify          bool
	minifier          *minify.M
)

var (
	books = []*Book{
		&Book{
			Title:     "Go",
			TitleLong: "Essential Go",
			Dir:       "go",
			// "https://www.notion.so/kjkpublic/Essential-Go-2cab1ed2b7a44584b56b0d3ca9b80185"
			NotionStartPageID: "2cab1ed2b7a44584b56b0d3ca9b80185",
		},
	}
)

const (
	// https://www.netlify.com/docs/headers-and-basic-auth/#custom-headers
	netlifyHeaders = `
# long-lived caching
/s/*
  Cache-Control: max-age=31536000
/*
  X-Content-Type-Options: nosniff
  X-Frame-Options: DENY
  X-XSS-Protection: 1; mode=block
`
)

const (
	googleAnalyticsTmpl = `<script async src="https://www.googletagmanager.com/gtag/js?id=%s"></script>
    <script>
        window.dataLayer = window.dataLayer || [];
        function gtag(){dataLayer.push(arguments);}
        gtag('js', new Date());
        gtag('config', '%s')
    </script>
`
)

func parseFlags() {
	flag.StringVar(&flgAnalytics, "analytics", "", "google analytics code")
	flag.BoolVar(&flgPreview, "preview", false, "if true will start watching for file changes and re-build everything")

	flag.BoolVar(&flgNoCache, "no-cache", false, "if true, disables cache for notion")
	flag.Parse()
}

func downloadBook(book *Book) {
	notionStartPageID := book.NotionStartPageID
	book.pageIDToPage = map[string]*notionapi.Page{}
	loadNotionPages(notionStartPageID, book.pageIDToPage, !flgNoCache)
	fmt.Printf("Loaded %d pages for book %s\n", len(book.pageIDToPage), book.Title)
	bookFromPages(book)
}

func iterPages(book *Book, onPage func(*Page) bool) {
	processed := map[string]bool{}
	toProcess := []*Page{book.RootPage}
	for len(toProcess) > 0 {
		page := toProcess[0]
		toProcess = toProcess[1:]
		id := normalizeID(page.NotionPage.ID)
		if processed[id] {
			continue
		}
		processed[id] = true
		toProcess = append(toProcess, page.Pages...)
		shouldContinue := onPage(page)
		if !shouldContinue {
			return
		}
	}
}

func loadSOUserMappingsMust() {
	path := filepath.Join("stack-overflow-docs-dump", "users.json.gz")
	err := common.JSONDecodeGzipped(path, &soUserIDToNameMap)
	u.PanicIfErr(err)
}

func genNetlifyHeaders() {
	path := filepath.Join("www", "_headers")
	err := ioutil.WriteFile(path, []byte(netlifyHeaders), 0644)
	panicIfErr(err)
}

func genNetlifyRedirects() {
	// add redirects for each book
	var a []string
	for _, b := range books {
		s := fmt.Sprintf(`/essential/%s/* /essential/%s/404.html 404`, b.Dir, b.Dir)
		a = append(a, s)
	}
	s := strings.Join(a, "\n")
	path := filepath.Join("www", "_redirects")
	err := ioutil.WriteFile(path, []byte(s), 0644)
	panicIfErr(err)
}

// TODO: probably more
func getDefaultLangForBook(bookName string) string {
	s := strings.ToLower(bookName)
	switch s {
	case "go":
		return "go"
	case "android":
		return "java"
	case "ios":
		return "ObjectiveC"
	case "microsoft sql server":
		return "sql"
	case "node.js":
		return "javascript"
	case "mysql":
		return "sql"
	case ".net framework":
		return "c#"
	}
	return s
}

func getBookDirs() []string {
	dirs, err := common.GetDirs("books")
	u.PanicIfErr(err)
	return dirs
}

func shouldCopyImage(path string) bool {
	return !strings.Contains(path, "@2x")
}

func copyFilesRecur(dstDir, srcDir string, shouldCopyFunc func(path string) bool) {
	createDirMust(dstDir)
	fileInfos, err := ioutil.ReadDir(srcDir)
	u.PanicIfErr(err)
	for _, fi := range fileInfos {
		name := fi.Name()
		if fi.IsDir() {
			dst := filepath.Join(dstDir, name)
			src := filepath.Join(srcDir, name)
			copyFilesRecur(dst, src, shouldCopyFunc)
			continue
		}

		src := filepath.Join(srcDir, name)
		dst := filepath.Join(dstDir, name)
		shouldCopy := true
		if shouldCopyFunc != nil {
			shouldCopy = shouldCopyFunc(src)
		}
		if !shouldCopy {
			continue
		}
		if pathExists(dst) {
			continue
		}
		copyFileMust(dst, src)
	}
}

func copyCoversMust() {
	copyFilesRecur(filepath.Join("www", "covers"), "covers", shouldCopyImage)
}

func getAlmostMaxProcs() int {
	// leave some juice for other programs
	nProcs := runtime.NumCPU() - 2
	if nProcs < 1 {
		return 1
	}
	return nProcs
}

func genAllBooks() {
	nProcs := getAlmostMaxProcs()

	timeStart := time.Now()
	clearSitemapURLS()
	copyCoversMust()

	copyToWwwAsSha1MaybeMust("main.css")
	copyToWwwAsSha1MaybeMust("app.js")
	copyToWwwAsSha1MaybeMust("favicon.ico")

	for _, book := range books {
		genBook(book)
	}
	writeSitemap()
	fmt.Printf("Used %d procs, finished generating all books in %s\n", nProcs, time.Since(timeStart))
}

func main() {
	parseFlags()

	//flgNoCache = true

	loadSOUserMappingsMust()

	os.RemoveAll("www")
	createDirMust(filepath.Join("www", "s"))
	genNetlifyHeaders()
	genNetlifyRedirects()

	//maybeRemoveNotionCache()
	for _, book := range books {
		book.titleSafe = common.MakeURLSafe(book.Title)
		downloadBook(book)
	}

	genAllBooks()

	if flgPreview {
		startPreview()
	}

}
