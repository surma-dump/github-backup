(function(){
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
