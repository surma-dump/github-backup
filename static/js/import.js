(function() {
  var btn = document.querySelector('#import button');
  btn.addEventListener('click', function() {
    var url = '/import?';
    [].forEach.call(document.querySelectorAll('#import input[type=checkbox]'), function(e) {
      if(e.checked) {
        url += e.id + '=true&';
      }
    });
    window.open(url);
  });
})();
