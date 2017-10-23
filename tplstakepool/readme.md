## Stakepool Theme Customization Files

This theme is already installed in the views/ and public/ directories of the document root.

If you want to customize the CSS and JS files you can do so by editing the files in the tplstakepool/src/ directories.

When you are finished with customization you have to build the CSS and JS files into the public/ directory in the document root.

### Prerequisites
* node.js
* npm

### Installation
Make sure you have node.js and npm installed.

```bash
cd $GOPATH/src/github.com/decred/dcrstakepool/tplstakepool
npm install
npm install -g bower
npm install -g grunt-cli
```

### Build the resources
In order to build the static files you have to run grunt tasks which will generate public/ directory in the document root with the static JS and CSS files, fonts and images.

This Process will overwrite the old Theme, including all Files in the public/ directory.

To build the full version run
```bash
grunt
```

