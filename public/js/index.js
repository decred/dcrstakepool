var stakepoolFinder = function() {

    var container = $("#stakepool-data");

    if (container.length == 0) {
        return;
    }
    
    var isMainnet = container.data("ismainnet");

    container.html("Loading...");
    tableMarkup = '<table id="pooldata" class="dtVerticalScroll table" cellspacing="0" width="100%"><thead class="thead-light"><tr class=""><th scope="col">ID</th><th scope="col">Address</th><th scope="col">Proportion <img src="/assets/images/arrow-up-down.svg" alt=""></th><th scope="col">VSP Fees <img src="/assets/images/arrow-up-down.svg" alt=""></th></tr></thead><tbody>';
    $.ajax({
        url: "https://api.decred.org/?c=gsd",
        dataType: "json",
        success: function(data) {
            $.each(data, function(poolName, poolData) {
                if (isMainnet) {
                    if (poolData["Network"] === 'testnet') {
                        return;
                    }
                } else {
                    if (poolData["Network"] === 'mainnet') {
                        return;
                    }
                }
                var proportion = poolData["ProportionLive"] * 100;
                tableMarkup += '<tr>';
                tableMarkup += '<td>' + poolName + '</td>';
                tableMarkup += '<td><a target="_blank" rel="noopener noreferrer" href="' + poolData["URL"] + '">' + poolData["URL"].replace("https://", "") + '</a></td>';
                tableMarkup += '<td>' + proportion.toFixed(2) + '%</td>';
                tableMarkup += '<td>' + poolData["PoolFees"] + '%</td>';
                tableMarkup += '</tr>';
            });
            tableMarkup += '</tbody></table>';
            container.html(tableMarkup);
            $('.dtVerticalScroll').DataTable({
                "scrollY": "251px",
                "scrollX": true,
                "scrollCollapse": true,
                "paging": false,
                "searching": false,
                "info": false
            });
            $('.dataTables_length').addClass('bs-select');
        },
    });
}();