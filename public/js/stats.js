var drawChart = function (containerID, keys, colors, data) {

    // create the svg canvas
    var svg = d3.select(containerID)
        .append("svg")
        .attr("width", 288)
        .attr("height", 350)
        .append("g")
        .attr("transform", "translate(" + 144 + "," + 190 + ")");

    // set the color scale
    var color = d3.scaleOrdinal()
        .domain(data)
        .range(colors);

    // draw a legend using dots and labels
    svg
        .selectAll("mydots")
        .data(keys)
        .enter()
        .append("circle")
        .attr("cx", -100)
        .attr("cy", function (d, i) { return -150 - i * 25 }) // -150 is where the first dot appears. 25 is the distance between dots.
        .attr("r", 7)
        .style("fill", function (d) { return color(d) });
        
    svg
        .selectAll("mylabels")
        .data(keys)
        .enter()
        .append("text")
        .attr("x", -80)
        .attr("y", function (d, i) { return -150 - i * 25 }) // -150 is where the first dot appears. 25 is the distance between dots.
        .style("fill", "#000")
        .text(function (d) { return d })
        .attr("text-anchor", "left")
        .style("font-size", 13)
        .style("alignment-baseline", "middle");
        
    // compute the position of each group on the pie
    var pie = d3.pie().value(function (d) { return d.value; });
    var data_ready = pie(d3.entries(data));

    // shape helper to build arcs
    var arcGenerator = d3.arc()
        .innerRadius(0)
        .outerRadius(125);
    
        // draw the pie chart
    svg
        .selectAll('pieSlices')
        .data(data_ready)
        .enter()
        .append('path')
        .attr('d', arcGenerator)
        .attr('fill', function (d) { return (color(d.data.key)) });

    // Now add the annotation. Use the centroid method to get the best coordinates
    svg
        .selectAll('pieSlices')
        .data(data_ready)
        .enter()
        .append('text')
        .text(function (d) { return d.data.value <= 0 ? "" : d.data.value })
        .attr("transform", function (d) { return "translate(" + arcGenerator.centroid(d) + ")"; })
        .style("text-anchor", "middle")
        .style("font-size", 15)
        .style("fill", "#fff");
};
