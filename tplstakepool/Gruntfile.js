module.exports = function(grunt) {
    grunt.initConfig({
        copy: {
            main: {
                files: [{
                        expand: true,
                        cwd: 'src/',
                        src: ['*'],
                        dest: '../public/',
                        filter: 'isFile'
                    }, {
                        expand: true,
                        cwd: 'src/js',
                        src: ['main.js', 'd3pie.min.js'],
                        dest: '../public/js',
                        filter: 'isFile'
                    },
                    {
                        expand: true,
                        cwd: 'src/svg',
                        src: ['sprites.svg'],
                        dest: '../public/svg',
                        filter: 'isFile'
                    },
                ]

            }
        },

        concat: {
            dist: {
                src: [
                    'bower_components/jquery/dist/jquery.min.js',
                    'bower_components/bootstrap-sass/assets/javascripts/bootstrap.min.js',
                    'src/js/main.js'
                ],
                dest: '../public/js/complete.js'
            }
        },


        uglify: {
            my_target: {
                files: {
                    '../public/js/complete.js': ['../public/js/complete.js']
                }
            }
        },
        sass: { // Task
            dist: { // Target
                options: { // Target options
                    style: 'expanded',
                    'sourcemap': 'none',
                    'style': 'compressed'

                },
                files: { // Dictionary of files
                    '../public/css/main.css': 'src/css/main.scss',

                }

            }
        },


        watch: {

            css: {
                files: ['src/css/main.scss'],
                tasks: ['sass'],
            },

            html: {
                files: ['src/*'],
                tasks: ['copy'],
            },

            img: {
                files: ['src/img/*.{png,jpg,gif}'],
                tasks: ['imagemin'],
                options: {
                    livereload: true
                }
            },
        }
    });



    grunt.loadNpmTasks('grunt-contrib-copy');
    grunt.loadNpmTasks('grunt-contrib-uglify');
    grunt.loadNpmTasks('grunt-contrib-concat');
    grunt.loadNpmTasks('grunt-contrib-sass');
    grunt.loadNpmTasks('grunt-contrib-watch');

    grunt.registerTask('default', ['copy', 'concat', 'uglify', 'sass', ]);
};
