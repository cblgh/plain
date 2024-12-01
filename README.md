# plain
_network markdown files into html with plaintext files_

![plain in use over at https://cblgh.org](https://cblgh.org/media/plain.png)

plain is a static-site generator operating on [plaintext
files](https://en.wikipedia.org/wiki/Plain_text) containing a small set of commands and
markdown input.

## Why
Original impetus:

* Server crashed leading to a lost web folder which contained manual-ish copied html files / pandoc'd wiki articles; many gone, post-crash
* Wanted something to republish markdown articles from my wiki to static html files, and update an article index
* Grew tired of my old website, mostly due to the markup—but honestly also the design

## What

plain revolves around converting individual markdown files into a network of html pages,
focusing on frictionless use.

Aside from markdown manipulation, plain enables you to copy entire directories into your
webroot while also connecting the copied directory to your other pages. You can author
conceptual articles in markdown, while being free to mix in the occasional bespoke page, when
the mood/humor/fever strikes.

Additional capabilities of plain include generating rss feeds for discrete
website categories, highlighting articles in the header nav, creating link-only
article dumps, automatically generate opengraph images for no-effort social
media link previews.

See the [code](https://github.com/cblgh/plain), or grab a [build](https://github.com/cblgh/plain/releases/).

## Usage
Typical use: 

```
plain --generate-previews --url https://cblgh.org; cp -r web/* /path/to/my/webroot   
```

Use the help flag to see all options:

```
plain -h
  -css string
        css stylesheet to copy into webdir (default "./style.css")
  -generate-previews
        generate experimental open-graph image previews
  -out string
        output path containing the assembled html (default "./web")
  -url string
        the canonical url of the hosted site; used primarily to generate rss feeds
  -v    toggle messages when running
```

## Features

* Generate [rss](https://en.wikipedia.org/wiki/RSS) for any number of listicles
* Convert markdown into html
* Copy directories into the webroot
* Bundles all files needed into a single executable
* Mod it: customize the command names by editing the `symbols` file
* Separate your files from your publishing; plain eschews [front matter](https://gohugo.io/content-management/front-matter/)

## Concepts

* The index file (think of it like a sitemap of sorts, or as a root listicle)
* Listicles (lists of articles, defined by commands and operands)
* Commands: symbols + operand (single, not plural)
* Navigation
* Directory copying
* Markdown first

For an example of how to construct a plain website, see the `/example` folder—or [cblgh.org](https://cblgh.org), for the deployed equivalent.

## Commands
```
tt  TITLE            title
bb  BRIEF            a one-line brief markdown description
md  PATH_MD          path to markdown file containing a standalone article / page
ln  LINK             link to resource representing the described item
ww  PATH_WWWROOT     set the final destination path in plain's webroot
cf  PATH_SSG         path to a listicle file containing ssg input (e.g. articles)
cp  COPY_DIR         copy an entire directory to the web root, preserving the folder name
nn  NAVIGATION_TITLE name navigation item & add to the main nav
mv  REDIRECT         redirect the given url (by dumping a redirect page) to the current item
cc  CREATE_RSS       create rss feed for listicle
//  SKIP             comment, skip parsing this line
``` 

Make plain your own by changing the command names (e.g. renaming `cc` -> `rss`) by editing the `symbols` file. The only
restriction is that the new command name may contain no spaces.


Currently some commands are only suitable for the index file, and some only for listicles.

```
listicle only
    none! :)
index only
    cf  PATH_SSG         path to a listicle file containing ssg input (e.g. articles) 
    cc  CREATE_RSS       create rss feed for listicle 
    nn  NAVIGATION_TITLE name navigation item & add to the main nav
both listicle & index
    tt  TITLE            title
    bb  BRIEF            a one-line brief markdown description
    md  PATH_MD          path to markdown file containing a standalone article / page
    ln  LINK             link to resource representing the described item
    ww  PATH_WWWROOT     set the final destination path in plain's webroot
    //  SKIP             comment, skip parsing this line
    cp  COPY_DIR         copy an entire directory to the web root, preserving the folder name 
    mv  REDIRECT         redirect the given url (by dumping a redirect page) to the current item
```
