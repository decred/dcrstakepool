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

$(function() {
    $('td:last-child input').change(function() {
        $(this).closest('tr').toggleClass("bg-checked", this.checked);
	});
});


$('.control-checkbox input').change(function () {
  if ($(this).is(":checked")) {
    $('.update-btn').addClass("d-flex");
  } else {
      var flag=0;
      $('.control-checkbox input').each(function(){
          if ($(this).is(":checked")) {
              $('.update-btn').addClass("d-flex");
            flag=1;             
          }
          if(flag == 0){
              $('.update-btn').removeClass('d-flex');
          }
      });
  }
});

$('.form-check-input').change(function () {
  if ($(this).is(":checked")) {
    $('.update-btn').addClass("d-flex");
  } else {
      var flag=0;
      $('.form-check-input').each(function(){
          if ($(this).is(":checked")) {
              $('.update-btn').addClass("d-flex");
            flag=1;             
          }
          if(flag == 0){
              $('.update-btn').removeClass('d-flex');
          }
      });
  }
});

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

//carousel dot tooltip setup
$(document).ready(function() {
  $('.carousel-nav__dot').tooltip({
    placement: 'top',
    trigger: 'hover',
    width: '100px'
  });
});

//flickity carousel setup
var $carousel = $('.main-carousel').flickity({
	cellAlign: 'left',
	contain: true,
	wrapAround: true,
	arrowShape: {
		x0: 10,
		x1: 60, y1: 50,
		x2: 60, y2: 40,
		x3: 60
	},
  prevNextButtons: false,
  pageDots: false
});

var flkty = $carousel.data('flickity');
var $cellButtonGroup = $('.carousel-nav');
//add slide buttons
var total = flkty.slides.length;

for (i = 0; i < total; i++) {
  var title = $('.carousel-cell').eq(i).find('h2').text();
  if(i === 0) {
     $cellButtonGroup.append('<li class="carousel-nav__dot is-selected" title="'+title+'"></li>');
   } else if(i === total - 1) {
    $cellButtonGroup.append('<li class="carousel-nav__dot mr-0" title="'+title+'"></li>');
   } else {
    $cellButtonGroup.append('<li class="carousel-nav__dot" title="'+title+'"></li>');
   }
}


$('.carousel-nav').prepend('<li class="carousel-nav__previous"><img src="/assets/images/arrow-prev.svg"></li>');
$('.carousel-nav').append('<li class="carousel-nav__next"><img src="/assets/images/arrow-next.svg"></li>');

var $cellButtons = $cellButtonGroup.find('.carousel-nav__dot');

// update selected cellButtons
$carousel.on( 'select.flickity', function() {
  $cellButtons.filter('.is-selected')
    .removeClass('is-selected');
  $cellButtons.eq( flkty.selectedIndex )
    .addClass('is-selected');
});

// select cell on button click
$cellButtonGroup.on( 'click', '.carousel-nav__dot', function() {
  var index = $(this).index() - 1;
  $carousel.flickity( 'select', index );
});

// previous
$('.carousel-nav__previous').on( 'click', function() {
  $carousel.flickity('previous');
});
// next
$('.carousel-nav__next').on( 'click', function() {
  $carousel.flickity('next');
});

