// A CloudFront Function on viewer requests (terraform/modules/static-site):
// sends www.${domain} to ${domain} with a permanent redirect, keeping the
// path and the query. As the bucket has no folders, an address that ends in
// a slash, such as /docs/, gets that folder's index.html, and one whose last
// part has no extension, such as /docs, is redirected to it with its slash.
function handler(event) {
  var request = event.request;
  var query = Object.keys(request.querystring)
    .map(function (key) {
      var parameter = request.querystring[key];
      var values = parameter.multiValue ? parameter.multiValue : [parameter];
      return values
        .map(function (value) {
          return value.value === "" ? key : key + "=" + value.value;
        })
        .join("&");
    })
    .join("&");
  var redirect = function (location) {
    return {
      statusCode: 301,
      statusDescription: "Moved Permanently",
      headers: { location: { value: location + (query ? "?" + query : "") } },
    };
  };
  if (request.headers.host.value === "www.${domain}") {
    return redirect("https://${domain}" + request.uri);
  }
  if (request.uri.endsWith("/")) {
    request.uri += "index.html";
    return request;
  }
  if (request.uri.slice(request.uri.lastIndexOf("/") + 1).indexOf(".") < 0) {
    return redirect(request.uri + "/");
  }
  return request;
}
