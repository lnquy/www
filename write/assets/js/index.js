// Configure the maximum number of search results you want to render here.
const resultNumToShow = 5;

var suggestions = document.getElementById('suggestions');
var userinput = document.getElementById('userinput');

document.addEventListener('keydown', inputFocus);

function inputFocus(e) {

  if (e.keyCode === 191 ) {
    e.preventDefault();
    userinput.focus();
  }

  if (e.keyCode === 27 ) {
    userinput.blur();
    suggestions.classList.add('d-none');
  }

}

document.addEventListener('click', function(event) {

  var isClickInsideElement = suggestions.contains(event.target);

  if (!isClickInsideElement) {
    suggestions.classList.add('d-none');
  }

});

/*
Source:
  - https://dev.to/shubhamprakash/trap-focus-using-javascript-6a3
*/

document.addEventListener('keydown',suggestionFocus);

function suggestionFocus(e){

  // Page scroll by Command+Up/Down combination on MacOS
  if (e.metaKey) {
    if (e.keyCode === 38) {
      window.scrollTo(0, 0);
    } else if (e.keyCode === 40) {
      window.scrollTo(0, document.body.scrollHeight);
    }
    return 
  }

  const focusableSuggestions= suggestions.querySelectorAll('a');
  const focusable= [...focusableSuggestions];
  const index = focusable.indexOf(document.activeElement);

  let nextIndex = 0;

  if (e.keyCode === 38) {
    e.preventDefault();
    nextIndex= index > 0 ? index-1 : 0;
    focusableSuggestions[nextIndex].focus();
  }
  else if (e.keyCode === 40) {
    e.preventDefault();
    nextIndex= index+1 < focusable.length ? index+1 : index;
    focusableSuggestions[nextIndex].focus();
  }

}


/*
Source:
  - https://github.com/nextapps-de/flexsearch#index-documents-field-search
  - https://raw.githack.com/nextapps-de/flexsearch/master/demo/autocomplete.html
*/

(function(){
  let indexingFields = [
    'title',
    'description',
    'content',
    'tags'
  ]

  let docIndex = new FlexSearch.Document({
    tokenize: "forward",
    optimize: true,
    resolution: 9,
    cache: 100,
    worker: false,
    document: {
        id: 'id',
        index: indexingFields
    },
  });

  // Build the list of all posts
  var docs = [
    {{ range $index, $page := (where .Site.Pages "Section" "blog") -}}
      {
        id: {{ $index }},
        href: "{{ .RelPermalink | relURL }}",
        title: {{ .Title | jsonify }},
        description: {{ .Params.description | jsonify }},
        content: {{ .Content | jsonify }},
        tags: {{ .Params.tags | jsonify }},
      },
    {{ end -}}
  ];

  // Normalize data before submit it into the indexing.
  // Generated posts may missing metadata like tags, categories.
  for (let i in docs) {
    if (!docs[i].tags) {
      docs[i].tags = []
    }

    // Indexing
    // console.log("DOC:", docs[i])
    docIndex.add(docs[i])
  }

  // console.log("INDEX:", docIndex);
  // let res = docIndex.search("totoro", {index: ["tags"]});
  // console.log("res", res)
  // console.log("store", docs[res[0].result[0]])

  userinput.addEventListener('input', show_results, true);
  suggestions.addEventListener('click', accept_suggestion, true);

  function show_results(){
    var value = this.value;
    var results = docIndex.search(value, {
      index: indexingFields,
      limit: resultNumToShow,
      offset: 0
    });
    // console.log("searchResult", results);

    if (!results || !results.length) {
      suggestions.innerHTML = ''; // TODO: Clear previous search result
      return
    }
    // Normalize returned results before rendering
    // hitMap[docId] = numberOfHits
    let hitMap = new Map();
    results.forEach(hit => {
      hit.result.forEach(docId => {
        let hitNum = hitMap.has(docId) ? hitMap.get(docId) : 0
        hitMap.set(docId, ++hitNum)
      })
    })
    // console.log('hitMap', hitMap)
    // Prioritize the page with more hits (descending)
    let hitMapDesc = new Map([...hitMap.entries()].sort((a, b) => {
      return b[1] - a[1]
    }))
    // console.log('hitMapDesc', hitMapDesc)

    suggestions.classList.remove('d-none');
    suggestions.innerHTML = ''; // TODO: Clear previous search result

    let renderedEntries = 0;
    for (let docId of hitMapDesc.keys()) { 
      let entry = document.createElement('div');
      entry.innerHTML = '<a href><span></span><span></span></a>';
      let a = entry.querySelector('a');
      let t = entry.querySelector('span:first-child');
      let d = entry.querySelector('span:nth-child(2)');

      let page = docs[docId];
      a.href = page.href;
      t.textContent = page.title;
      d.textContent = page.description;

      suggestions.appendChild(entry);

      if (++renderedEntries > resultNumToShow) {
        break
      }
    }
  }

  function accept_suggestion(){
    while(suggestions.lastChild){
      suggestions.removeChild(suggestions.lastChild);
    }
    return false;
  }

}());
