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
	"github.com/cblgh/plain/og"
	"github.com/cblgh/plain/rss"
	"github.com/cblgh/plain/util"
	"github.com/gomarkdown/markdown"
	"io"
	"io/fs"
	"net/url"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// TODO (2022-11-04):
//   - add ability to set attribute on e.g. <a> elements such that i can do:
//     ln cblgh.org
//     attr rel="me" => <a href="cblgh.org" rel="me">
var verbose = true

func echo(s ...interface{}) {
	if verbose {
		fmt.Println(s...)
	}
}

// git repo ideas:
// * set all git repos under <webroot>/_git/<reponame>.git
// * <reponame> is taken as the last path component of the passed in repository
// * faffing about needed:
//		* create bare repo: git clone --bare <path-reponame> <webroot>/_git/<reponame>.git
//		* in bare repo: hooks/post-update.sample move to hooks/post-update.sample
//		* in bare repo: execute git update-server-info
//    * in src repo: execute git add remote local <webroot/_git/reponame.git>
//		* in src repo: .git/hooks/post-commit should exist, be executable, and run `git push local main`
//
// detect "readme.md"; detect if first line is a title, inject "Get the code: git clone git.<canonicalurl>/<reponame.git>
// play around with rendering latest commit info

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
// gt git repo
// br git branch
// vb verbatim - verbatim copy a file and dump it at destination

const (
	/* tt */ TITLE = iota
	/* bb */ BRIEF
	/* ln */ LINK
	/* // */ SKIP
	/* nn */ NAVIGATION_TITLE
	/* cf */ PATH_SSG
	/* md */ PATH_MD
	/* vb */ VERBATIM
	/* ww */ PATH_WWWROOT
	/* cp */ COPY_DIR
	/* mv */ REDIRECT /* redirects a something.html to a /something/index.html route
	/* as */ALIAS /* redirects from a route /something to a route /entirely-something-else (defined by md)*/
	/* rn */RENAME /* renames a filename from the input source to a completely new filename, decoupling source filename from route name */
	/* un */ UNDER_CATEGORY
	/* cc */ CREATE_RSS
	/* bg */ BACKGROUND
	/* sf */ FOREGROUND_COLOR
	/* sb */ BACKGROUND_COLOR
	/* hi */ HEADER_IMAGE
	/* sl */ LINK_COLOR
	/* gt */ GIT_REPO
	/* br */ GIT_BRANCH
	/* xx */ NOIDEA
)

type feedDescription struct {
	name, description string
	nested bool
}

type Pair struct {
	code    string
	content string
}

type Element struct {
	pairs []Pair
}

type Theme struct {
	foreground string
	background string
	link       string
}

type PageFragment struct {
	theme              Theme
	underParent				 bool
	title, brief, link string
	background         string
	webpath, contents  string
	location           string
	metadata					 []string
}

type Page struct {
	html          []string
	headerContent []string
	pf            PageFragment
	parentDir		  bool
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
		case "VERBATIM":
			return VERBATIM
		case "REDIRECT":
			return REDIRECT
		case "ALIAS":
			return ALIAS
		case "RENAME":
			return RENAME
		case "UNDER_CATEGORY":
			return UNDER_CATEGORY
		case "CREATE_RSS":
			return CREATE_RSS
		case "BACKGROUND":
			return BACKGROUND
		case "FOREGROUND_COLOR":
			return FOREGROUND_COLOR
		case "BACKGROUND_COLOR":
			return BACKGROUND_COLOR
		case "HEADER_IMAGE":
			return HEADER_IMAGE
		case "LINK_COLOR":
			return LINK_COLOR
		case "GIT_REPO":
			return GIT_REPO
		case "GIT_BRANCH":
			return GIT_BRANCH
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

var wikilinksPattern = regexp.MustCompile(`(\[\[(.*?)\]\])`)

func transformWikilinks(content []byte) []byte {
	s := string(content)
	matches := wikilinksPattern.FindAllStringSubmatch(s, -1)
	// search and replace all instances of [[wiki]] syntax with a flat link to the subject e.g. /wiki
	if len(matches) > 0 {
		for _, match := range matches {
			s = strings.ReplaceAll(s, match[1], fmt.Sprintf(`<a href="/%s">%s</a>`, strings.ToLower(match[2]), match[2]))
		}
	}
	return []byte(s)
}

func markup(s string) string {
	return string(markdown.ToHTML([]byte(strings.TrimSpace(s)), nil, nil))
}

func (pf PageFragment) assemble() string {
	// if listicle entry omits title, don't list it as a listicle item (it's a hidden page)
	if len(pf.title) == 0 {
		return ""
	}
	if len(pf.link) > 0 {
		return fmt.Sprintf(
			`<dt><a href="%s">%s</a></dt>
    <dd>%s</dd>
   `,
			pf.link, pf.title, markup(pf.brief))
	} else {
		return fmt.Sprintf(`
    <dt>%s</dt>
    <dd>%s</dd>`,
			pf.title, markup(pf.brief))
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

var titlePattern = regexp.MustCompile(`(<title>(.*)<\/title>)`)

func htmlContent(content string) string {
	return fmt.Sprintf("<main><article>%s</article></main>", content)
}

func htmlPreamble(pf PageFragment) string {
	prevRoute := pf.webpath
	var mainNav string

	if prevRoute != "" {
		returnName := strings.TrimPrefix(prevRoute, "/")
		if returnName == "" {
			returnName = "home"
		}
		mainNav += fmt.Sprintf(`<li><a href="%s">Back to %s</a></li>`, prevRoute, returnName)
	} else {
		mainNav = "<li></li> "
	}
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

	// add background image to an article if it has been set
	const backgroundSentinel = "<!-- background -->"
	const themeSentinel = "<!-- theme -->"
	const backgroundTemplate = `
  <style>
  html {
    background-image: url("%s");
  }
  </style>
  `
	if pf.background != "" {
		bg := fmt.Sprintf(backgroundTemplate, pf.background)
		header = strings.ReplaceAll(header, backgroundSentinel, bg)
	} else {
		header = strings.ReplaceAll(header, backgroundSentinel, "")
	}
	if pf.theme.foreground != "" || pf.theme.background != "" || pf.theme.link != "" {
		var theme string
		if pf.theme.foreground != "" {
			theme += fmt.Sprintf("--foreground: %s !important;", pf.theme.foreground)
		}
		if pf.theme.background != "" {
			theme += fmt.Sprintf("--background: %s !important;", pf.theme.background)
		}
		if pf.theme.link != "" {
			theme += fmt.Sprintf("--highlight: %s !important;", pf.theme.link)
		}

		rootStyle := fmt.Sprintf(`
    :root {
      %s
    }
    `, theme)

		if pf.theme.link != "" {
			rootStyle += fmt.Sprintf(`
      a {
        color: %s !important;
      }`, pf.theme.link)
		}
		style := fmt.Sprintf(`<style>%s</style`, rootStyle)
		header = strings.ReplaceAll(header, themeSentinel, style)
	} else {
		header = strings.ReplaceAll(header, backgroundSentinel, "")
	}
	var htmlMeta string
	// augment html meta tags and titles with article metadata.
	// grab unaugmented <title>
	match := titlePattern.FindStringSubmatch(header)
	if len(match) >= 3 {
		if pf.title != "" {
			htmlMeta += fmt.Sprintf(`<title>%s — %s</title>%s`, pf.title, match[2], "\n")
		}
		if pf.brief != "" {
			htmlMeta += fmt.Sprintf(`<meta name="description" content="%s">%s`, pf.brief, "\n")
		}

		// generate opengraph metadata and image
		if generateOG && pf.title != "" {
			_, articleName := extractFilenames(pf.location)
			// if rewrittenDest != "" {
			//   articleName = rewrittenDest
			// }
			imageName := fmt.Sprintf("%s.png", strings.ReplaceAll(strings.ToLower(articleName), " ", "-"))
			imagePath := filepath.Join(OUTPATH, "og", imageName)
			canonicalPath := fmt.Sprintf("%s/og/%s", canonicalUrl, imageName)
			err = os.MkdirAll(filepath.Dir(imagePath), 0777)
			util.Check(err)

			settings := og.GetDefaultSettings()
			htmlMeta += og.GenerateMetadata(pf.title, pf.brief, canonicalPath, settings)
			// og.GenerateImage(pf.title, pf.brief, imagePath, settings)
		}
	}
	// add other metadata, such as the experimental vcs discovery meta tags for repos
	if len(pf.metadata) > 0 {
		htmlMeta += strings.Join(pf.metadata, "\n")
	}

	if htmlMeta != "" {
		header = strings.Replace(header, match[1], htmlMeta, -1)
	}
	return fmt.Sprintf(`%s
  <nav>
    <ul class="main-navigation">
    %s
    </ul>
  </nav>`, header, mainNav)
}

func htmlEpilogue() string {
	footer, err := readTemplate("footer.html", DEFAULT_FOOTER)
	if err != nil {
		log.Fatalln(err)
	}
	return footer
}

var OUTPATH = filepath.Join(".", "web")

func extractPageFragments(webpath string, underParent bool, elements []Element) []string {
	// TODO: do 2 pass to identify alternate write paths for PATH_MD / COPY_DIR, as set by LINK tag?
	var html []string
	html = append(html, "<dl class='listicle'>")
	for _, el := range elements {
		pf := PageFragment{webpath: webpath, underParent: underParent}
		pf.metadata = make([]string, 0)
		var rewrittenDest string
		branchName := "master" // used for GIT_REPO
		// var background string
		for _, p := range el.pairs {
			switch symbol(p.code) {
			case GIT_BRANCH:
				branchName = p.content
			case PATH_WWWROOT:
				rewrittenDest = p.content
			case TITLE:
				pf.title = p.content
			case BRIEF:
				pf.brief = p.content
			case BACKGROUND:
				pf.background = p.content
			case BACKGROUND_COLOR:
				pf.theme.background = p.content
			case FOREGROUND_COLOR:
				pf.theme.foreground = p.content
			case LINK_COLOR:
				pf.theme.link = p.content
			case LINK:
				if pf.link != "" {
					echo(fmt.Sprintf("err: already set link on page fragment? %v\n", el.pairs))
					continue
				}
				pf.link = p.content
			}
		}

		for _, p := range el.pairs {
			switch symbol(p.code) {
			case GIT_REPO:
				setupBareRepo(p.content, filepath.Join(OUTPATH, "_git"), branchName)
				repoName := filepath.Base(p.content)
				stats := produceRepoStatistics(p.content, filepath.Join(OUTPATH, "_git"))

				if pf.title == "" {
					pf.title = repoName
				}

				clonePath := fmt.Sprintf(`http://git.%s/%s.git`, host, repoName)
				// support VCS Autodiscovery (https://git.sr.ht/~ancarda/vcs-autodiscovery-rfc)
				pf.metadata = append(pf.metadata, `<meta name="vcs" content="git" />`)
				pf.metadata = append(pf.metadata, fmt.Sprintf(`<meta name="vcs:default-branch" content="%s" />`, branchName))
				pf.metadata = append(pf.metadata, fmt.Sprintf(`<meta name="vcs:clone" content="%s" />`, clonePath))
				pf.metadata = append(pf.metadata, fmt.Sprintf(`<meta name="forge:summary" content="https://%s/%s">`, canonicalUrl, repoName))

				// check for readme variants to render
				readmeVariations := []string{"README.md", "readme.md", "README"}
				checkReadmeExists := func(p string) bool {
					_, err := os.Stat(p)
					if err != nil && errors.Is(err, os.ErrNotExist) {
						return false
						// alright this is the case when we want to continue! :)
					}
					return true
				}
				for _, readme := range readmeVariations {
					readmePath := filepath.Join(p.content, readme)
					exists := checkReadmeExists(readmePath)
					rewrittenDest = repoName
					if exists {
						pf.location = readmePath
						// yank'd out of CopyMarkdownFile so we can inject the git clone instruction
						filename, _ := extractFilenames(pf.location)
						md, err := ReadMarkdownFile(filename)
						util.Check(err)
						lines := strings.Split(md.contents, "\n")
						injected := fmt.Sprintf(`<div id="clone"><span>%s</span><span>git clone %s</span></div>`, stats, clonePath)
						if strings.Contains(lines[0], "<h1>") {
							newLines := []string{lines[0], injected}
							newLines = append(newLines, lines[1:]...)
							md.contents = strings.Join(newLines, "\n")
						} else {
							newLines := []string{injected}
							newLines = append(newLines, lines...)
							md.contents = strings.Join(newLines, "\n")
						}
						err = WriteMarkdownAsHTML(pf, rewrittenDest, md)
						util.Check(err)

						_, articleName := extractFilenames(p.content)
						if rewrittenDest != "" {
							articleName = rewrittenDest
						}
						pf.link = filepath.Join("/", articleName)
						break
					}
				}
			case COPY_DIR:
				// copy a directory from one place and into plain's webroot
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
			case VERBATIM:
				// TODO (2024-04-27): MAKE THIS WORK
				// INCLUDING COMPOSING WELL WITH THE REWRITE-Y COMMANDS LIKE 
				// `un`

				// copy a file from one place and into plain's webroot
				echo("copying directory at", p.content)
				if p.content == "/" || p.content == "~" {
					echo(fmt.Sprintf("tried to copy '%s'; stopped the operation as it seems unlikely to be correct :)", p.content))
					continue
				}
				base := filepath.Base(p.content)
				dstpath := filepath.Join(OUTPATH, base)
				copyFile(p.content, dstpath)
				if rewrittenDest != "" {
					base = rewrittenDest
				}
				pf.link = filepath.Join("/", base)
			case PATH_MD:
				// source a markdown file from one place and output a corresponding html site in plain's webroot
				pf.location = p.content
				err := CopyMarkdownFile(pf, rewrittenDest)
				if err != nil {
					continue
				}
				_, articleName := extractFilenames(p.content)
				if rewrittenDest != "" {
					articleName = rewrittenDest
				}
				pf.link = filepath.Join("/", articleName)
				if pf.underParent {
					pf.link = filepath.Join("/", pf.webpath, articleName)
				}
			case REDIRECT:
				err := DumpRedirectFile(p.content)
				util.Check(err)
			case ALIAS:
				err := DumpAliasFile(p.content, pf.link)
				util.Check(err)
			case RENAME:
				err := RenameFile(pf.link, p.content)
				pf.link = filepath.Join("/", p.content)
				util.Check(err)
			}
		}
		html = append(html, pf.assemble())
	}
	html = append(html, "</dl>")
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
func CopyMarkdownFile(pf PageFragment, rewrittenDest string) error {
	filename, _ := extractFilenames(pf.location)
	md, err := ReadMarkdownFile(filename)
	if err != nil {
		return err
	}
	err = WriteMarkdownAsHTML(pf, rewrittenDest, md)
	return err
}

//go:embed default/redirect-template.html
var REDIRECT_TEMPLATE string

//go:embed default/alias-template.html
var ALIAS_TEMPLATE string

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

func RenameFile(oldpath, newpath string) error {
	oldpath = filepath.Join(OUTPATH, oldpath)
	newpath = filepath.Join(OUTPATH, newpath)
	info, err := os.Stat(newpath)
	if errors.Is(err, os.ErrNotExist) {
	// the destination does not exist, great! we can proceed. if it's some other kind of error, we should just throw it
	}
	if info != nil && info.IsDir() {
		err = os.RemoveAll(newpath)
		if err != nil {
			return err
		}
	}
	// if we get a fileinfo back we know that the directory exists and we basically want to overwrite it -> let's do it
	err = os.Rename(oldpath, newpath)
	if err != nil {
		return err
	}
	// make sure the old dir is removed
	err = os.Remove(oldpath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func DumpAliasFile(aliasPath, webpath string) error {
	var outfile string
	var dst string

	dst = webpath
	// we'll create outfile as it's the alias that will be visited intially (which will redirect to `dst`)
	outfile = filepath.Join(OUTPATH, aliasPath, "index.html")
	// make sure we have the appropriate folder structure
	err := os.MkdirAll(filepath.Join(OUTPATH, aliasPath), 0777)
	if err != nil {
		return err
	}
	// but first: make sure we're not clobbering something that's already there
	_, err = os.Stat(outfile)
	// ok: we're not clobbering anything, time to dump stuff
	if errors.Is(err, os.ErrNotExist) {
		aliasInstance := strings.ReplaceAll(ALIAS_TEMPLATE, "$SENTINEL$", dst)
		err = os.WriteFile(outfile, []byte(aliasInstance), 0666)
		return err
	} else if err != nil {
		return err
	}
	return nil
}

// the "markdown" we're writing has actually already been parsed as html, so what we're writing is really just html. but
// i think this name is more representative of what we're doing: persisting what was a markdown file in one location, as
// a new html file in another location
func WriteMarkdownAsHTML(pf PageFragment, rewrittenDest string, md mdFile) error {
	filename, articleName := extractFilenames(pf.location)
	if rewrittenDest != "" {
		articleName = rewrittenDest
	}
	outfile := filepath.Join(OUTPATH, articleName, "index.html")
	if pf.underParent {
		outfile = filepath.Join(OUTPATH, pf.webpath, articleName, "index.html")
	}

	echo("try to open", filename)
	err := os.MkdirAll(filepath.Dir(outfile), 0777)
	if err != nil {
		return err
	}

	if len(md.images) > 0 {
		err = persistImages(pf.location, md)
		if err != nil {
			return err
		}
	}

	echo("writing file contents to", outfile)
	err = os.WriteFile(outfile, []byte(wrap(pf, md.contents)), 0666)
	return err
}


func persistImages (baseLocation string, md mdFile) error {
	echo("persisting images")
	mediabase := "media"
	mediadir := filepath.Join(OUTPATH, mediabase)
	// make sure the <OUTPATH>/media dir exists
	err := os.MkdirAll(mediadir, 0777)
	if err != nil {
		return err
	}
	// copy all images from their source to mediadir
	for _, img := range md.images {
		base := strings.Split(filepath.ToSlash(baseLocation), "/")[0]
		src := filepath.Join(base, img)
		dst := filepath.Join(mediadir, filepath.Base(img))
		echo(fmt.Sprintf("copying %s to %s\n", src, dst))
		copyFile(src, dst)
	}
	md.rewriteImageUrls(mediabase)
	return nil
}

func ReadMarkdownFile(filename string) (mdFile, error) {
	b, err := os.ReadFile(strings.TrimSpace(filename))
	if err != nil {
		return mdFile{}, err
	}
	paths := extractImagePaths(b)
	b = transformWikilinks(b)
	return mdFile{contents: string(markdown.ToHTML(b, nil, nil)), images: paths}, nil
}

func produceRepoStatistics (repoSrcPath, dst string) string {
	cwd, err := os.Getwd()
	util.Check(err)
	bareRepoPath := filepath.Join(cwd, dst, fmt.Sprintf("%s.git", filepath.Base(repoSrcPath)))
	echo("git repo statistics", bareRepoPath)

	// equivalent to running the following in a bash script:
	// COMMITS=$(git rev-list --count HEAD)
	// SIZE=$(git count-objects -H | cut -d',' -f2-)
	// FILES=$(git ls-tree --full-tree -r HEAD | wc -l)

	// count commits
	cmd := exec.Command("git", "rev-list", "--count", "HEAD")
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Dir = bareRepoPath

	err = cmd.Run()
	if err != nil {
		log.Fatalln(err)
	}
	commits := strings.TrimSpace(out.String())
	out.Reset()
	echo("commits counted")

	// get repo size
	cmd = exec.Command("git", "count-objects", "-H")
	cmd.Stdout = &out
	cmd.Dir = bareRepoPath

	err = cmd.Run()
	if err != nil {
		log.Fatalln(err)
	}
	size := strings.TrimSpace(strings.Split(out.String(), ",")[1])
	out.Reset()
	echo("repo size estimated")

	// count files
	cmd = exec.Command("git", "ls-tree", "--full-tree", "-r", "HEAD")
	cmd.Stdout = &out
	cmd.Dir = bareRepoPath

	err = cmd.Run()
	if err != nil {
		log.Fatalln(err)
	}
	count := strings.Count(out.String(), "\n")
	out.Reset()
	echo("files counted")

	return fmt.Sprintf("%s commits, %d files, %s", commits, count, size)
}

func setupBareRepo(repoSrcPath, dst, defaultBranch string) {
	// make sure we have _git base folder
	err := os.MkdirAll(dst, 0777)
	util.Check(err)
	cwd, err := os.Getwd()
	util.Check(err)
	bareRepoPath := filepath.Join(cwd, dst, fmt.Sprintf("%s.git", filepath.Base(repoSrcPath)))
	echo("git bare repo", bareRepoPath)
	// check if we've already setup the repo
	_, err = os.Stat(bareRepoPath)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		// alright this is the case when we want to continue! :)
	} else if err == nil {
		return
	}

	// --bare cloning
	cmd := exec.Command("git", "clone", "--bare", repoSrcPath, bareRepoPath)
	var out strings.Builder
	cmd.Stderr = &out
	err = cmd.Run()
	if err != nil {
		log.Fatalln(err)
	}
	echo("git clone:", out.String())
	out.Reset()

	// moving post-update
	updateHook := filepath.Join(bareRepoPath, "hooks", "post-update")
	err = os.Rename(fmt.Sprintf("%s.sample", updateHook), updateHook)
	if err != nil {
		fmt.Println("failed to rename post-update.sample")
		log.Fatalln(err)
	} else {
		echo("post-update hook enabled")
	}

	// running git update-serve-info
	cmd = exec.Command("git", "update-server-info")
	cmd.Dir = bareRepoPath
	err = cmd.Run()
	if err != nil {
		fmt.Println("failed to run update-server-info")
		log.Fatalln(err)
	} else {
		echo("update-server-info done")
	}

	// creating local remote
	cmd = exec.Command("git", "remote", "add", "local", bareRepoPath)
	cmd.Dir = repoSrcPath
	err = cmd.Run()
	if err != nil {
		fmt.Println("failed to add remote local")
	} else {
		echo("remote local added")
	}

	// write post-commit hook
	commitHook := fmt.Sprintf(`#!/bin/bash
	git push local %s
	`, defaultBranch)
	err = os.WriteFile(filepath.Join(repoSrcPath, ".git", "hooks", "post-commit"), []byte(commitHook), 0777)
	if err != nil {
		fmt.Println("failed to add post-commit to source repository")
		log.Fatalln(err)
	} else {
		echo("post-commit hook written")
	}
}

func wrap(pf PageFragment, html string) string {
	return fmt.Sprintf(`%s %s %s`, htmlPreamble(pf), htmlContent(html), htmlEpilogue())
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

func headerImageTemplate (imgPath string) string {
	return fmt.Sprintf(`
	<div>
	<img class="header-image" src="%s">
	</div>
	`, imgPath)
}

func processRootListicle(elements []Element) {
	var feeds []feedDescription
	var pages = make(map[string]Page) // a mapping from the declared page route to the page object
	// do two pass scan to populate the navigation elements
	// TODO: find all other dependencies (ww?)
	// first pass
	for _, el := range elements {
		var navEl navigation
		var listicleName string
		var nestUnderParent bool
		for _, p := range el.pairs {
			switch symbol(p.code) {
			case UNDER_CATEGORY:
				nestUnderParent = true
			case PATH_SSG:
				listicleName = p.content
			case CREATE_RSS:
				if listicleName == "" {
					fmt.Println("plain: listicle name was empty! did the create_rss (cc) directive come before the listicle declaration (cf)?")
				}
				feeds = append(feeds, feedDescription{name: listicleName, nested: nestUnderParent, description: p.content})
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
			case UNDER_CATEGORY:
				page.parentDir = true
			case PATH_WWWROOT:
				// TODO (2023-02-02): remove page.webpath bc now duplicate of pf
				page.pf.webpath = p.content
			case TITLE:
				page.headerContent = append(page.headerContent, markup(p.content))
				page.pf.title = util.SanitizeMarkdown(p.content)
			case HEADER_IMAGE:
				dstPath := filepath.Join("/media", filepath.Base(p.content))
				page.headerContent = append(page.headerContent, headerImageTemplate(dstPath))
				persistImages(p.content, mdFile{images: []string{dstPath}})
			case BRIEF:
				page.headerContent = append(page.headerContent, markup(p.content))
				page.pf.brief = util.SanitizeMarkdown(p.content)
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
				if len(md.images) > 0 {
					persistImages(p.content, md)
				}
				page.html = append(page.html, md.contents)
			case PATH_SSG:
				resource := readListicle(p.content)
				// implicitly dependent on ww declared before cf command
				if page.pf.webpath == "" {
					echo(fmt.Sprintf("error [grouping #%d]: cf (%s) declared before ww for pair %v\n", i+1, p.content, el.pairs))
					os.Exit(0)
				}
				page.html = append(page.html, extractPageFragments(page.pf.webpath, page.parentDir, resource)...)
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
		if pagePrev, ok := pages[page.pf.webpath]; ok {
			pagePrev.html = append(pagePrev.html, insertSpacer())
			pagePrev.html = append(pagePrev.html, page.headerContent...)
			page.html = append(pagePrev.html, page.html...)
			// don't overwrite the previous title
			page.pf.title = pagePrev.pf.title
		} else {
			page.html = append(page.produceHeader(), page.html...)
		}
		pages[page.pf.webpath] = page
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

const ListicleTemplate = `tt %s.xml
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
	// route and page.pf.webpath are equivalent, route's just shorter
	for route, page := range pages {
		// we have this case if we e.g. only want to copy a folder
		if len(page.html) == 0 {
			continue
		}
		dirname := filepath.Join(OUTPATH, strings.TrimPrefix(route, "/"))
		filename := filepath.Join(dirname, "index.html")
		err := os.MkdirAll(dirname, 0777)
		util.Check(err)
		page.pf.webpath = createHistoryLink(route)
		html := wrap(page.pf, strings.Join(page.html, ""))
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
var host string
var generateOG bool
var symbols map[string]int

func main() {
	populateFiles()

	var cssPath string
	flag.BoolVar(&generateOG, "generate-previews", false, "generate experimental open-graph image previews")
	flag.StringVar(&OUTPATH, "out", "./web", "output path containing the assembled html")
	flag.StringVar(&cssPath, "css", "./style.css", "css stylesheet to copy into webdir")
	flag.StringVar(&canonicalUrl, "url", "", "the canonical url of the hosted site; used primarily to generate rss feeds")
	flag.BoolVar(&verbose, "v", false, "toggle messages when running")
	flag.Parse()

	// make sure canonical url has a scheme. http-centric for now, change if it ever is raised as an issue
	if !strings.HasPrefix(canonicalUrl, "http") {
		canonicalUrl = fmt.Sprintf("https://%s", canonicalUrl)
	}
	u, err := url.Parse(canonicalUrl)
	util.Check(err)
	host = u.Host

	parseSymbols()
	err = os.MkdirAll(OUTPATH, 0777)
	util.Check(err)
	index := readListicle("index")
	processRootListicle(index)
	copyFile(cssPath, filepath.Join(OUTPATH, "style.css"))
}

/* rss-ish stuff */
var rssmap map[string]rss.FeedItem

const rfc822RSS = "Mon, 02 Jan 2006 15:04:05 -0700"

func extractListicleFeedPosts(listicle, nested, canonicalURL string) []rss.FeedItem {
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
				linkPath := strings.TrimSuffix(filepath.Base(p.content), ".md")
				if nested != "" {
					linkPath = fmt.Sprintf("%s/%s", nested, linkPath)
				}
				pf.link = util.ConstructURL(canonicalURL, linkPath)
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
		var nestedPath string
		if listicle.nested {
			nestedPath = listicle.name
		}
		items := extractListicleFeedPosts(listicle.name, nestedPath, canonicalURL)
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
