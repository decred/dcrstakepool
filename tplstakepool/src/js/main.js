$(function(){
$("#inputPassword3").data('holder',$("#inputPassword3").attr('placeholder'));
$("#inputPassword3").focusin(function(){
    $(this).attr('placeholder','');
});
$("#inputEmail3").data('holder',$("#inputEmail3").attr('placeholder'));
$("#inputEmail3").focusin(function(){
    $(this).attr('placeholder','');
});
$("#inputEmail3,#inputPassword3").focusout(function(){
    $(this).attr('placeholder',$(this).data('holder'));
});

})

$(document).ready(function(){
    $('#ticketslive').DataTable({
	responsive: true
    });
    $('#ticketsvoted').DataTable({
	responsive: true
    });
    $('#ticketsmissed').DataTable({
	responsive: true
    });
});
