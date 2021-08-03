package main

// original impetus
// * server crashed -> lost web dir folder with manual-ish copied over html files / pandoc'd wiki articles
// * wanted something to republish markdown articles from my wiki to static html files, and update an index over them
// * was tired of my old website, mostly due to the markup. but honestly also the design
// * the design is not impacted by this generation.. really wanna make something inspired by
//   https://merveilles.town/@thomasorus/106456722974843498
// * wanted to try out something oscean-like, without copying devine's design and ideas wholesale—cause that'd be no fun

// project name: plain
import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"github.com/cblgh/plain/rss"
	"github.com/cblgh/plain/util"
	"github.com/gomarkdown/markdown"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// flags (imaginary, thus far)
// --media
//    media root folder (used to copy over assets from e.g. a personal wiki)

// TO DO:
// DONE   don't hardcode alexander cobleigh in <title>; read header preamble from file?
// DONE   don't hardcode webring icon in footer
// after basics are finished:
// DONE   --out flag
// DONE   implement cp command to copy entire folder to new destination
// DONE   --css flag
// DONE   some solution for copying media files that aren't monotome-based (/media)
// DONE   add rss support -> update feed whenever new items detected? use a canonical json file to write rss file?
// DONE   save rss files in <OUTPATH/*.xml>
// DONE   output a feeds/index.html file, listing the generated feeds
// DONE   make ww work for listcles and root listicle alike
// DONE   define file with custom tags to longer-form names & enable people to define their own names for tags
// implement support for..
// in index:
//  DONE    generate redirect
//  DONE    copy directory
// in listicles:
//  * generate navigation? might be hard
//
// stretchies:
// properly indent html output?

var verbose = true

func echo(s ...interface{}) {
	if verbose {
		fmt.Println(s...)
	}
}

// tt title
// bb oneline brief markdown description
// md path to markdown file for longer descriptions, or entire page content
// ln link to resource representing the described item
// ww path in webroot
// cf path containing ssg input (e.g. articles)
// cp copy an entire directory to the web root, preserving the folder name
// nn name navigation item & add to the main nav
// mv redirect the given url (by dumping a redirect page) to the current item
// cc create rss feed for listicle
// // comment, skip this

const (
	/* tt */ TITLE = iota
	/* bb */ BRIEF
	/* ln */ LINK
	/* // */ SKIP
	/* nn */ NAVIGATION_TITLE
	/* cf */ PATH_SSG
	/* md */ PATH_MD
	/* ww */ PATH_WWWROOT
	/* cp */ COPY_DIR
	/* mv */ REDIRECT
	/* cc */ CREATE_RSS
	/* xx */ NOIDEA
)

type feedDescription struct {
	name, description string
}

type Pair struct {
	code    string
	content string
}

type Element struct {
	pairs []Pair
}

type PageFragment struct {
	title, brief, link string
	webpath, contents  string
}

type Page struct {
	html          []string
	webpath       string
	headerContent []string
	// todo: tab title
}

type mdFile struct {
	contents string
	images   []string // slice of image paths, for later copying
}

type navigation struct {
	link string
	text string
}

func parseSymbols() {
	symbols = make(map[string]int)
	input, err := os.ReadFile("symbols")
	util.Check(err)
	parseConstant := func(s string) int {
		switch s {
		case "TITLE":
			return TITLE
		case "BRIEF":
			return BRIEF
		case "LINK":
			return LINK
		case "SKIP":
			return SKIP
		case "NAVIGATION_TITLE":
			return NAVIGATION_TITLE
		case "PATH_SSG":
			return PATH_SSG
		case "PATH_MD":
			return PATH_MD
		case "PATH_WWWROOT":
			return PATH_WWWROOT
		case "COPY_DIR":
			return COPY_DIR
		case "REDIRECT":
			return REDIRECT
		case "CREATE_RSS":
			return CREATE_RSS
		default:
			return NOIDEA
		}
	}
	for _, line := range strings.Split(string(input), "\n") {
		if len(line) == 0 {
			continue
		}
		parts := strings.Fields(string(line))
		command, constant := parts[0], parts[1]
		symbols[command] = parseConstant(constant)
	}
}

func symbol(line string) int {
	line = strings.TrimSpace(line)
	if len(line) == 0 {
		return SKIP
	}
	if sym, exists := symbols[strings.Fields(line)[0]]; exists {
		return sym
	}
	return NOIDEA
}

func (page Page) produceHeader() []string {
	if len(page.headerContent) == 0 {
		return []string{}
	}

	return []string{fmt.Sprintf(`<header>
%s
</header>
  `, strings.Join(page.headerContent, "\n"))}
}

var markdownImagePattern = regexp.MustCompile(`[!]\[.*\]\((\S+)\)`)

func extractImagePaths(content []byte) []string {
	s := string(content)
	var paths []string
	matches := markdownImagePattern.FindAllStringSubmatch(s, -1)
	if len(matches) > 0 {
		for _, match := range matches {
			// discard http[s]? matches; we can't very well copy them :)
			if strings.HasPrefix(match[1], "https://") || strings.HasPrefix(match[1], "http://") {
				continue
			}
			paths = append(paths, match[1])
		}
	}
	return paths
}

func markup(s string) string {
	return string(markdown.ToHTML([]byte(strings.TrimSpace(s)), nil, nil))
}

func (pf PageFragment) assemble() string {
	if len(pf.link) > 0 {
		return fmt.Sprintf(`<h2 class="listicle"><a href="%s">%s</a></h2>
%s
`, pf.link, pf.title, markup(pf.brief))
	} else {
		return fmt.Sprintf(`<h2 class="listicle">%s</h2>
%s
`, pf.title, markup(pf.brief))
	}
}

func readTemplate(template, defaultContent string) (string, error) {
	_, err := createIfNotExist(template, defaultContent)
	if err != nil {
		util.Check(err)
	}
	b, err := os.ReadFile(template)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func htmlPreamble(prevRoute string) string {
	var returnLink string
	if prevRoute != "" {
		returnName := strings.TrimPrefix(prevRoute, "/")
		if returnName == "" {
			returnName = "home"
		}
		returnLink = fmt.Sprintf(`<p><a href="%s">Back to %s</a></p>`, prevRoute, returnName)
	}
	var mainNav string
	for _, nav := range navElements {
		if nav.text == "" {
			continue
		}
		mainNav += fmt.Sprintf(`<li><a href="%s">%s</a></li>`, nav.link, nav.text)
	}
	header, err := readTemplate("header.html", DEFAULT_HEADER)
	if err != nil {
		log.Fatalln(err)
	}
	return fmt.Sprintf(`%s
  <ul class="main-navigation">
  %s
  </ul>
  <nav>%s</nav>`, header, mainNav, returnLink)
}

func htmlEpilogue() string {
	footer, err := readTemplate("footer.html", DEFAULT_FOOTER)
	if err != nil {
		log.Fatalln(err)
	}
	return footer
}

var OUTPATH = filepath.Join(".", "web")

func extractPageFragments(webpath string, elements []Element) []string {
	// TODO: do 2 pass to identify alternate write paths for PATH_MD / COPY_DIR, as set by LINK tag?
	var html []string
	for _, el := range elements {
		pf := PageFragment{}
		var rewrittenDest string
		for _, p := range el.pairs {
			switch symbol(p.code) {
			case PATH_WWWROOT:
				rewrittenDest = p.content
			}
		}

		for _, p := range el.pairs {
			switch symbol(p.code) {
			case TITLE:
				pf.title = p.content
			case BRIEF:
				pf.brief = p.content
			case LINK:
				if pf.link != "" {
					echo(fmt.Sprintf("err: already set link on page fragment? %v\n", el.pairs))
					continue
				}
				pf.link = p.content
				// copy a directory from one place and into plain's webroot
			case COPY_DIR:
				echo("copying directory at", p.content)
				if p.content == "/" || p.content == "~" {
					echo(fmt.Sprintf("tried to copy '%s'; stopped the operation as it seems unlikely to be correct :)", p.content))
					continue
				}
				err := CopyDirectory(p.content, OUTPATH, rewrittenDest)
				util.Check(err)
				base := filepath.Base(p.content)
				if rewrittenDest != "" {
					base = rewrittenDest
				}
				pf.link = filepath.Join("/", base)
				// source a markdown file from one place and output a corresponding html site in plain's webroot
			case PATH_MD:
				err := CopyMarkdownFile(p.content, rewrittenDest, webpath)
				if err != nil {
					continue
				}
				_, articleName := extractFilenames(p.content)
				if rewrittenDest != "" {
					articleName = rewrittenDest
				}
				pf.link = filepath.Join("/", articleName)
			case REDIRECT:
				err := DumpRedirectFile(p.content)
				util.Check(err)
			}
		}
		html = append(html, pf.assemble())
	}
	return html
}

var ignored = []string{".git", "node_modules"}

func containsIgnored(s string) bool {
	for _, ignoredString := range ignored {
		if ignoredString == s {
			return true
		}
	}
	return false
}

// Copy the contents of a directory to the webroot, preserving the directory's basename.
// Traverses readDir, copying files to the writeDir (of the form: filepath.Join(OUTPATH, filepath.Base(readDir)))
func CopyDirectory(readDir, writeDir, rewrittenDest string) error {
	base := filepath.Base(readDir)
	if rewrittenDest != "" {
		base = rewrittenDest
	}
	dst := filepath.Join(writeDir, base)
	files, err := os.ReadDir(readDir)
	if err != nil {
		return err
	}
	err = os.MkdirAll(dst, 0777)
	if err != nil {
		return err
	}
	// perform os.Create / os.Mkdir at dst (and not at writeDir)
	for _, f := range files {
		if f.IsDir() && !containsIgnored(f.Name()) {
			CopyDirectory(filepath.Join(readDir, f.Name()), dst, "")
		} else {
			srcfile, err := os.Open(filepath.Join(readDir, f.Name()))
			if err != nil {
				return err
			}
			dstfile, err := os.Create(filepath.Join(dst, f.Name()))
			if err != nil {
				return err
			}
			io.Copy(dstfile, srcfile)
			srcfile.Close()
			dstfile.Close()
		}
	}
	return nil
}

// processes the location and extracts the article name from the location, with the file md suffix & initial path removed
func extractFilenames(location string) (string, string) {
	return strings.TrimSpace(location), strings.TrimSuffix(filepath.Base(location), ".md")
}

func (md *mdFile) rewriteImageUrls(mediadir string) {
	for _, image := range md.images {
		md.contents = strings.ReplaceAll(md.contents, image, filepath.Join("/", mediadir, filepath.Base(image)))
	}
}

// copies markdown file at location, returns strings.TrimSuffix(filepath.Base(location), ".md")
func CopyMarkdownFile(location, rewrittenDest, webpath string) error {
	filename, _ := extractFilenames(location)
	md, err := ReadMarkdownFile(filename)
	if err != nil {
		return err
	}
	err = WriteMarkdownAsHTML(location, rewrittenDest, webpath, md)
	return err
}

//go:embed default/redirect-template.html
var REDIRECT_TEMPLATE string

// The mv command to redirects from older routes to the declared one
// examples:
// md wiki/exjobb/trustnet.md
// mv /articles/trustnet.html   creates a folder "articles", if it doesn't exist, and dumps the redirect in its "trustnet.html"
//
// md wiki/life/support.md
// mv /support.html   dumps a "support.html" in the web dir
// mv /about          creates a folder "about" & dumps the redirect in its index.html

func DumpRedirectFile(webpath string) error {
	var outfile string
	var dirStructure string
	// redirecting a html-suffixed file, e.g. /web/articles/cool-article.html
	if strings.HasSuffix(webpath, ".html") {
		// grab everything leading up until (but excluding) the .html file
		dirStructure = strings.TrimSpace(strings.TrimPrefix(filepath.Dir(webpath), "/"))
		outfile = filepath.Join(OUTPATH, webpath)
	} else {
		// we're redirecting a different path, e.g. /web/articles/cool-article/
		dirStructure = webpath
		outfile = filepath.Join(OUTPATH, webpath, "index.html")
	}
	// make sure we have the appropriate folder structure
	if len(dirStructure) > 0 {
		err := os.MkdirAll(filepath.Join(OUTPATH, dirStructure), 0777)
		if err != nil {
			return err
		}
	}
	// but first: make sure we're not clobbering something that's already there
	_, err := os.Stat(outfile)
	// ok: we're not clobbering anything, time to dump stuff
	if errors.Is(err, os.ErrNotExist) {
		err = os.WriteFile(outfile, []byte(REDIRECT_TEMPLATE), 0666)
		return err
	} else if err != nil {
		return err
	}
	return nil
}

// the "markdown" we're writing has actually already been parsed as html, so what we're writing is really just html. but
// i think this name is more representative of what we're doing: persisting what was a markdown file in one location, as
// a new html file in another location
func WriteMarkdownAsHTML(location, rewrittenDest, webpath string, md mdFile) error {
	filename, articleName := extractFilenames(location)
	if rewrittenDest != "" {
		articleName = rewrittenDest
	}
	outfile := filepath.Join(OUTPATH, articleName, "index.html")

	echo("try to open", filename)
	err := os.MkdirAll(filepath.Dir(outfile), 0777)
	if err != nil {
		return err
	}
	if len(md.images) > 0 {
		echo("copying images")
		mediabase := "media"
		mediadir := filepath.Join(OUTPATH, mediabase)
		// make sure the <OUTPATH>/media dir exists
		err := os.MkdirAll(mediadir, 0777)
		if err != nil {
			return err
		}
		// copy all images from their source to mediadir
		for _, img := range md.images {
			base := strings.Split(filepath.ToSlash(location), "/")[0]
			src := filepath.Join(base, img)
			dst := filepath.Join(mediadir, filepath.Base(img))
			echo(fmt.Sprintf("copying %s to %s\n", src, dst))
			copyFile(src, dst)
		}
		md.rewriteImageUrls(mediabase)
	}
	echo("writing file contents to", outfile)
	err = os.WriteFile(outfile, []byte(wrap(webpath, md.contents)), 0666)
	return err
}

func ReadMarkdownFile(filename string) (mdFile, error) {
	b, err := os.ReadFile(strings.TrimSpace(filename))
	if err != nil {
		return mdFile{}, err
	}
	paths := extractImagePaths(b)
	return mdFile{contents: string(markdown.ToHTML(b, nil, nil)), images: paths}, nil
}

func wrap(webpath, html string) string {
	return fmt.Sprintf(`%s %s %s`, htmlPreamble(webpath), html, htmlEpilogue())
}

func readListicle(filename string) []Element {
	b, err := os.ReadFile(filename)
	util.Check(err)
	lines := strings.Split(string(b), "\n")

	var el Element
	var elements []Element
	for _, line := range lines {
		// newline detected (newline delineates individual elements / pair groupings)
		if strings.TrimSpace(line) == "" && len(el.pairs) > 0 {
			elements = append(elements, el)
			el = Element{}
			continue
		}
		if symbol(line) == SKIP {
			continue
		}
		code := strings.Fields(line)[0]
		content := strings.TrimSpace(line[len(code):])
		el.pairs = append(el.pairs, Pair{code: code, content: content})
	}
	return elements
}

var navElements []navigation

func processRootListicle(elements []Element) {
	var feeds []feedDescription
	var pages = make(map[string]Page) // a mapping from the declared page route to the page object
	// do two pass scan to populate the navigation elements
	// TODO: find all other dependencies (ww?)
	// first pass
	for _, el := range elements {
		var navEl navigation
		var listicleName string
		for _, p := range el.pairs {
			switch symbol(p.code) {
			case PATH_SSG:
				listicleName = p.content
			case CREATE_RSS:
				if listicleName == "" {
					fmt.Println("plain: listicle name was empty! did the create_rss (cc) directive come before the listicle declaration (cf)?")
				}
				feeds = append(feeds, feedDescription{name: listicleName, description: p.content})
			case NAVIGATION_TITLE:
				navEl.text = p.content
			case PATH_WWWROOT:
				navEl.link = p.content
			default:
				continue
			}
		}
		navElements = append(navElements, navEl)
	}

	// output listicle enumerating rss feeds
	if len(feeds) > 0 {
		feeds = append(feeds, feedDescription{name: "all", description: fmt.Sprintf("all of %s", util.TrimUrl(canonicalUrl))})
		OutputFeedsListicle(feeds)
	}

	// second pass: generate the content && html
	for i, el := range elements {
		var page Page
		for _, p := range el.pairs {
			switch symbol(p.code) {
			case PATH_WWWROOT:
				page.webpath = p.content
			case TITLE, BRIEF:
				page.headerContent = append(page.headerContent, markup(p.content))
				page.headerContent = append(page.headerContent, markup(p.content))
			case COPY_DIR:
				echo("copying directory at", p.content)
				if p.content == "/" || p.content == "~" {
					echo(fmt.Sprintf("tried to copy '%s'; stopped the operation as it seems unlikely to be correct :)", p.content))
					continue
				}
				err := CopyDirectory(p.content, OUTPATH, "")
				util.Check(err)
			case PATH_MD: // change to work the same way as for regular listicles
				md, err := ReadMarkdownFile(p.content)
				if err != nil {
					echo(fmt.Errorf("%w", err))
					continue
				}
				page.html = append(page.html, md.contents)
			case PATH_SSG:
				resource := readListicle(p.content)
				// implicitly dependent on ww declared before cf command
				if page.webpath == "" {
					echo(fmt.Sprintf("error [grouping #%d]: cf (%s) declared before ww for pair %v\n", i+1, p.content, el.pairs))
					os.Exit(0)
				}
				page.html = append(page.html, extractPageFragments(page.webpath, resource)...)
			case REDIRECT:
				err := DumpRedirectFile(p.content)
				util.Check(err)
			case SKIP:
				fallthrough
			default:
				continue
			}
		}

		// we're inserting another ssg page into an already registered page, add some spacing
		// to visually separate them
		if pageTemp, ok := pages[page.webpath]; ok {
			pageTemp.html = append(pageTemp.html, insertSpacer())
			pageTemp.html = append(pageTemp.html, page.headerContent...)
			page.html = append(pageTemp.html, page.html...)
		} else {
			page.html = append(page.produceHeader(), page.html...)
		}
		pages[page.webpath] = page
	}

	// create rss files for all feeds
	if len(feeds) > 0 {
		if canonicalUrl == "" {
			fmt.Println("plain: specified rss generation, but the canonical url flag (--url) is not set")
			echo("not writing rss feeds")
		} else {
			GenerateFeeds(feeds, canonicalUrl)
		}
	}

	// write all html to files
	persistToFS(pages)
}

const ListicleTemplate = `tt %s
bb %s
ln /%s.xml

`

func OutputFeedsListicle(listicles []feedDescription) error {
	var output string
	for _, listicle := range listicles {
		output += fmt.Sprintf(ListicleTemplate, listicle.name, listicle.description, listicle.name)
	}
	return os.WriteFile("feeds", []byte(output), 0666)
}

// to do: use <section> and style that instead :)
// at the same time: introduce <main> after <header>
func insertSpacer() string {
	return `<div class="spacer"></div>` + "\n"
}

func persistToFS(pages map[string]Page) {
	for route, page := range pages {
		// we have this case if we e.g. only want to copy a folder
		if len(page.html) == 0 {
			continue
		}
		dirname := filepath.Join(OUTPATH, strings.TrimSpace(strings.TrimPrefix(route, "/")))
		filename := filepath.Join(dirname, "index.html")
		err := os.MkdirAll(dirname, 0777)
		util.Check(err)
		webpath := createHistoryLink(route)
		html := fmt.Sprintf("%s\n%s\n%s", htmlPreamble(webpath), strings.Join(page.html, ""), htmlEpilogue())
		err = os.WriteFile(filename, []byte(html), 0666)
		util.Check(err)
	}
}

func createHistoryLink(k string) string {
	webpathParts := strings.Split(k, "/")
	webpath := "/" // default to linking back to the root
	if len(webpathParts) > 2 {
		webpath = strings.TrimSpace(strings.Join(webpathParts[:len(webpathParts)-1], "/"))
	}
	// don't link back to anything (we're at the root, or home page)
	if strings.TrimSpace(k) == "/" {
		webpath = ""
	}
	return webpath
}

//go:embed default/default-symbols
var DEFAULT_SYMBOLS string

//go:embed default/default-header.html
var DEFAULT_HEADER string

//go:embed default/default-footer.html
var DEFAULT_FOOTER string

//go:embed default/default-style.css
var DEFAULT_CSS string

//go:embed example/example-index
var EXAMPLE_INDEX string

//go:embed example/example-listicle
var EXAMPLE_PAGE string

//go:embed example/example-contacts
var EXAMPLE_CONTACTS string

func createIfNotExist(name, content string) (bool, error) {
	_, err := os.Stat(name)
	if err != nil {
		// if the file doesn't exist, create it
		if errors.Is(err, fs.ErrNotExist) {
			err = os.WriteFile(name, []byte(content), 0777)
			if err != nil {
				return false, err
			}
			// created file successfully
			return true, nil
		} else {
			return false, err
		}
	}
	return false, nil
}

func copyFile(src, dst string) {
	reader, err := os.Open(src)
	util.Check(err)
	writer, err := os.Create(dst)
	util.Check(err)
	_, err = io.Copy(writer, reader)
	util.Check(err)
	writer.Close()
	reader.Close()
}

func populateFiles() {
	firstTimeUse, err := createIfNotExist("index", EXAMPLE_INDEX)
	if err != nil {
		util.Check(err)
	}
	// it's likely the first time someone is using the tool; let's bump out a couple of example files as well :)
	if firstTimeUse {
		createIfNotExist("projects", EXAMPLE_PAGE)
		createIfNotExist("contacts", EXAMPLE_CONTACTS)
	}
	createIfNotExist("style.css", DEFAULT_CSS)
	createIfNotExist("symbols", DEFAULT_SYMBOLS)
}

var canonicalUrl string
var symbols map[string]int

func main() {
	populateFiles()

	var cssPath string
	flag.StringVar(&OUTPATH, "out", "./web", "output path containing the assembled html")
	flag.StringVar(&cssPath, "css", "./style.css", "css stylesheet to copy into webdir")
	flag.StringVar(&canonicalUrl, "url", "", "the canonical url of the hosted site; used primarily to generate rss feeds")
	flag.BoolVar(&verbose, "v", false, "toggle messages when running")
	flag.Parse()

	parseSymbols()
	err := os.MkdirAll(OUTPATH, 0777)
	util.Check(err)
	index := readListicle("index")
	processRootListicle(index)
	copyFile(cssPath, filepath.Join(OUTPATH, "style.css"))
}

/* rss-ish stuff */
var rssmap map[string]rss.FeedItem

const rfc822RSS = "Mon, 02 Jan 2006 15:04:05 -0700"

func extractListicleFeedPosts(listicle, canonicalURL string) []rss.FeedItem {
	pubdate := time.Now()
	elements := readListicle(listicle)

	var feed []rss.FeedItem
	for _, el := range elements {
		pf := PageFragment{}
		for _, p := range el.pairs {
			switch symbol(p.code) {
			case TITLE:
				pf.title = p.content
			case BRIEF:
				pf.brief = util.SanitizeMarkdown(p.content)
			case PATH_MD:
				pf.link = util.ConstructURL(canonicalURL, strings.TrimSuffix(filepath.Base(p.content), ".md"))
			case LINK:
				if !strings.HasPrefix(p.content, "http") {
					pf.link = util.ConstructURL(canonicalURL, p.content)
				} else {
					pf.link = p.content
				}
			}
		}
		if len(pf.link) > 0 {
			id := filepath.Join(listicle, pf.title)
			item := rss.FeedItem{Pubdate: pubdate.Unix()}
			if _, exists := rssmap[id]; exists {
				// replace with previously stored rss.FeedItem
				item = rssmap[id]
			} else {
				// we're generating this for the first time
				item.RSSItem = rss.OutputRSSItem(pubdate.Format(rfc822RSS), pf.title, pf.brief, pf.link)
				rssmap[id] = item
			}
			feed = append(feed, item)
		}
	}
	return feed
}

// when generating a listicle feed:
//  read the listcle
//  construct an id per listicle item
//  check if the id exists in the map
//    if id already exists -> get rss.FeedItem{} from map
//    otherwise -> construct new rss.FeedItem{}, and add to map
//
// after all listcles have been processed, dump the current map to rss-store.json

func GenerateFeeds(listicles []feedDescription, canonicalURL string) {
	rssmap = rss.OpenStore()
	dumpFeed := func(name, desc string, items []rss.FeedItem) {
		shortUrl := util.TrimUrl(canonicalURL)
		title := fmt.Sprintf("%s - %s", shortUrl, name)
		rssOutput := rss.OutputRSS(title, canonicalURL, desc, rss.GetItems(items))
		rss.SaveFeed(OUTPATH, fmt.Sprintf("%s.xml", name), rssOutput)
	}
	// combined represents a single rss feed of all the listicle feeds e.g. projects + articles
	var combined []rss.FeedItem
	for _, listicle := range listicles {
		if listicle.name == "all" {
			continue
		}
		items := extractListicleFeedPosts(listicle.name, canonicalURL)
		dumpFeed(listicle.name, listicle.description, items)
		combined = append(combined, items...)
	}
	// sort combined's posts by latest pubdate
	sort.Slice(combined, func(i, j int) bool {
		return combined[i].Pubdate > combined[j].Pubdate
	})
	dumpFeed("all", fmt.Sprintf("all of %s", canonicalURL), combined)
	rss.SaveStore(rssmap)
}
