//mobile menu triggering
$('#dismiss').on('click', function () {
    $('#sidebar, .menu-trigger').removeClass('active');
});

$('#sidebarCollapse').on('click', function () {
    $('#sidebar, .menu-trigger').addClass('active');
});

//disables the form submit buttons until the inputs are filled in
function submitState(elSelector) {

    var $form = $(elSelector),
        $requiredInputs = $form.find('input:required'),
        $submit = $form.find('input[type="submit"]');

    $submit.attr('disabled', 'disabled');

    $requiredInputs.keyup(function () {

      $form.data('empty', 'false');

      $requiredInputs.each(function() {
        if ($(this).val() === '') {
          $form.data('empty', 'true');
        }
      });

      if ($form.data('empty') === 'true') {
        $submit.attr('disabled', 'disabled').attr('title', 'fill in all required fields');
      } else {
        $submit.removeAttr('disabled').attr('title', 'click to submit');
      }
    });
}
submitState('#Login');
submitState('#Register');
submitState('#Password');
submitState('#Reset');
submitState('#ChangeEmail');
submitState('#ChangePassword');
submitState('#captcha-form');

// hide input error when input's value changes
$('.err-form-control').on("change paste keyup", function() {
  // reset styling of input
  $(this).removeClass('err-form-control'); 
  // hides the error icon
  $(this).next().fadeOut();
  // remove error text
  $(this).parent().next().fadeOut();
});

$(document).ready(function () {
  // Display elements with class js-only
  var els = document.getElementsByClassName("js-only")
  for (var i = 0; i < els.length; i++) {
    els[i].classList.remove('d-none')
  }
});

$(document).ready(function () {
  // display close buttons on snackbar notifications
  $('.snackbar-close-button-top').removeClass('d-none');
  // add click listener to close buttons
  $('.snackbar-close-button-top').on('click', function(e){
    $(this).parent().parent().fadeOut();
  });
});
