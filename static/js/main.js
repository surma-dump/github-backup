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

  Q.xhr.get('/repos').then(function(resp) {
    resp.data.forEach(function(e) {
      var l = document.createElement('label');
      l.setAttribute('data-value', e);
      l.textContent = e;
      var i = document.createElement('input');
      i.type = 'checkbox';
      l.insertBefore(i, l.childNodes[0]);
      Polymer.dom(filter).appendChild(l);
    });
  }).then(function() {
    return Q.xhr.get('/active');
  }).then(function(resp) {
    resp.data.forEach(function(e) {
      Polymer.dom(filter).querySelector('[data-value="' + e + '"] input').checked = true;
    });
  });
})();
