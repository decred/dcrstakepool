(function () {
    // Change checkbox state if user clicks anywhere on the row.
    // Not just the actual checkbox
    var rows = document.getElementsByTagName("tr")
    for (var i = 0; i < rows.length; i++) {
        rows[i].addEventListener("click", function (e) {
            if (e.target.tagName == "A") return;
            box = this.querySelector("input[type=checkbox]");
            box.checked = !box.checked
        });
    }

})();