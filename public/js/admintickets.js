(function () {
    // Change checkbox state if user clicks anywhere on the row.
    // Not just the actual checkbox
    var rows = document.getElementsByTagName("tr")
    for (var i = 0; i < rows.length; i++) {
        rows[i].addEventListener("click", function (e) {
            if (e.target.tagName == "A") return;
            box = this.querySelector("input[type=checkbox]");
            if (box.checked) {
                $(box).closest('tr').removeClass('bg-checked');
            } else {
                $(box).closest('tr').addClass('bg-checked');
            }
            box.checked = !box.checked
        });
    }

})();

//controls tables with selections and checkboxes
var selectMsg = "Select all";
var deselectMsg = "Deselect all";

$("#select_all_ignored").click(toggleCheckBoxes("#ignored_table"));
$("#select_all_added").click(toggleCheckBoxes("#added_table"));

function toggleCheckBoxes(tableId) {
    return function (clicked) {
        var table = $(tableId);
        if ($(this)[0].text == deselectMsg) {
            table.find(':checkbox').prop('checked', false);
            table.find('tr').removeClass('bg-checked');
            $(this).text(selectMsg);
            $('.update-btn').removeClass("d-flex");
        }
        else {
            table.find(':checkbox').prop('checked', true);
            table.find('tr').addClass('bg-checked');
            $(this).text(deselectMsg);
            $('.update-btn').addClass('d-flex');
        }
    };
}

$(function () {
    $('td:last-child input').change(function () {
        $(this).closest('tr').toggleClass("bg-checked", this.checked);
    });
});

$(function () {
    $('td:last-child input').change(function () {
        $(this).closest('tr').toggleClass("bg-checked", this.checked);
    });
});

$('.control-checkbox input').change(function () {
    if ($(this).is(":checked")) {
        $('.update-btn').addClass("d-flex");
    } else {
        var flag = 0;
        $('.control-checkbox input').each(function () {
            if ($(this).is(":checked")) {
                $('.update-btn').addClass("d-flex");
                flag = 1;
            }
            if (flag == 0) {
                $('.update-btn').removeClass('d-flex');
            }
        });
    }
});