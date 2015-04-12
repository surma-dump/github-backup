(function() {
  var selector = document.querySelector("#filter > input");
  var filter = document.querySelector("#filter > x-filter");
  selector.addEventListener('input', function() {
    filter.selector = selector.value;
  });

  filter.addEventListener('change', function(ev) {
    var input = ev.target;
    var item = input.parentElement;
    input.disabled = true;
    var action = 'activate';
    if(!input.checked) {
      action = 'deactivate';
    }
    Q.xhr.get('/' + action + '?name=' + item.getAttribute('data-value')).then(function() {
      input.disabled = false;
    });
  });
})();
