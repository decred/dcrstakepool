## Install
# Step 1 (Optional, when customizing css):
```
$ cd $GOPATH/src/github.com/decred/dcrstakepool/tplstakepool
$ npm install
$ bower install
$ grunt
```
# Step 2
Edit your dcrstakepool.conf  and point to the public and views directory to activate the template
```
publicpath=tpl_stakepool/public
templatepath=tpl_stakepool/views
```
# Step 3
Restart dcrstakepool
