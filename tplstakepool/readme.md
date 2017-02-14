## Stakepool Theme Customization Files

This Theme is allready installed in the views/ and public/ Directory of the document root.

If you want to customize the css and js files you can do so by editing the files in the tplstakepool/src/ directories.

When you are finished with customization you have to build the css and js files into the public/ directory in the document root.

### Prequisite
* node
* npm

### Installation
Make sure you have node.js and npm installed.

```bash
cd $GOPATH/src/github.com/decred/dcrstakepool/tplstakepool
npm install
npm install -g bower
npm install -g grunt-cli
```

### Build the ressources
In order to build the static files you have to run grunt tasks which will generate public/ directory in the document root with the static js and css files, fonts and images.

This Process will overwrite the old Theme, including all Files in the public/ directory.

To build the full version run
```bash
grunt
```

