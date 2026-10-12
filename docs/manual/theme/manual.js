// A link back to the Astrolabe CLI landing page at the left of the menu bar.
(function () {
    var bar = document.querySelector('.left-buttons');
    if (!bar || bar.querySelector('.astro-home')) return;
    var a = document.createElement('a');
    a.className = 'astro-home';
    a.href = '../';
    a.textContent = 'astrolabe';
    a.title = 'Astrolabe CLI home page';
    bar.appendChild(a);
})();
